/**
 * @license
 * Copyright (c) 2018 Cisco and/or its affiliates.
 *
 * This software is licensed to you under the terms of the Cisco Sample
 * Code License, Version 1.0 (the "License"). You may obtain a copy of the
 * License at
 *
 *                https://developer.cisco.com/docs/licenses
 *
 * All use of the material herein must be in accordance with the terms of
 * the License. All rights not expressly granted by the License are
 * reserved. Unless required by applicable law or agreed to separately in
 * writing, software distributed under the License is distributed on an "AS
 * IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express
 * or implied.
 */
package controller

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"github.com/golang/protobuf/proto"
	xr "github.com/nleiva/xrgrpc"
	"github.com/sfloresk/tviewer/proto/telemetry"
	ifcs "github.com/sfloresk/tviewer/proto/telemetry/interface"
	isis "github.com/sfloresk/tviewer/proto/telemetry/isis"
	"github.com/sfloresk/tviewer/model"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"time"
)

type Node struct {
	Name     string
	Ip       string
	Username string
	Password string
	CertName string
	Port     string
}

func (node Node) CollectInterfaceData(interfaceChannel chan model.TelemetryWrapper) {
	// Open database
	session, err := mgo.Dial(os.Getenv("TELEMETRY_DB"))
	if err != nil {
		panic(err)
	}
	defer session.Close()

	// Switch the session to a monotonic behavior.
	session.SetMode(mgo.Monotonic, true)

	dbCollection := session.DB("Telemetry").C("Interfaces")

	// Clean database from previous data
	err = dbCollection.Remove(bson.M{})

	// Variable for output formatting

	flag.Parse()

	// Determine the ID for first the transaction.
	var id int64 = 1000

	// Manually specify target parameters.
	router1, err := xr.BuildRouter(
		xr.WithUsername(node.Username),
		xr.WithPassword(node.Password),
		xr.WithHost(node.Ip + ":" + node.Port),
		xr.WithCert(node.CertName),
		xr.WithTimeout(10000),
	)
	if err != nil {
		log.Fatalf("target parameters for router are incorrect: %s", err)
	}

	// Connect to the target
	conn1, ctx1, err := xr.Connect(*router1)
	if err != nil {
		log.Fatalf("could not setup a client connection to %s, %v", router1.Host, err)
	}
	defer conn1.Close()

	id++
	ctx1, cancel := context.WithCancel(ctx1)
	defer cancel()
	c := make(chan os.Signal, 1)
	// If no signals are provided, all incoming signals will be relayed to c.
	// Otherwise, just the provided signals will. E.g.: signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	signal.Notify(c, os.Interrupt)
	defer func() {
		signal.Stop(c)
		cancel()
	}()

	// encoding GPB
	var e int64 = 2

	ch, ech, err := xr.GetSubscription(ctx1, conn1, ifSubscriptionID, id, e)

	if err != nil {
		log.Fatalf("could not setup Telemetry Subscription: %v\n", err)
	}

	go func() {
		select {
		case <-c:
			fmt.Printf("\nmanually cancelled the session to %v\n\n", router1.Host)
			cancel()
			return
		case <-ctx1.Done():
		// Timeout: "context deadline exceeded"
			err = ctx1.Err()
			fmt.Printf("\ngRPC session timed out after %v seconds: %v\n\n", router1.Timeout, err.Error())
			return
		case err = <-ech:
		// Session canceled: "context canceled"
			fmt.Printf("\ngRPC session to %v failed: %v\n\n", router1.Host, err.Error())
			return
		}
	}()

	for tele := range ch {
		result := make([]model.TelemetryMessage, 0)
		message := new(telemetry.Telemetry)

		var previousTs uint64 = 0
		err := proto.Unmarshal(tele, message)
		if err != nil {
			log.Fatalf("Could not unmarshall the interface telemetry message for %v: %v\n", node.Name, err)
		}

		ts := message.GetMsgTimestamp()
		changed := false

		for _, row := range message.GetDataGpb().GetRow() {
			// Get the message content
			content := row.GetContent()

			// Create a new container object
			ifaceInt := new(ifcs.FibShInt)

			err = proto.Unmarshal(content, ifaceInt)

			if err != nil {
				log.Fatalf("Could not decode content in the interface telemetry message for %v: %v\n", node.Name, err)
			}

			// Get interface name and IP
			ifName := ifaceInt.GetPerInterface()
			ifaceIntIp := ifaceInt.GetPrimaryIpv4Address()

			if (ifaceIntIp != "UNKNOWN" && ifaceIntIp != "NOT PRESENT") {
				// Only saves the ones that have an IP

				// Create InterfaceTelemetry struct
				newIfTele := model.InterfaceTelemetry{
					TimeStamp: ts,
					NodeName: node.Name,
					Interface: ifName,
					Ip: ifaceIntIp,
				}


				// Check if exists in database
				dbResult := model.InterfaceTelemetry{}

				count, err := dbCollection.Find(bson.M{
					"nodename": node.Name,
					"interface":ifName,
				}).Count()

				if (count > 0) {
					err = dbCollection.Find(bson.M{
						"nodename": node.Name,
						"interface":ifName,
					}).One(&dbResult)

					// Save the old timestamp
					previousTs = dbResult.TimeStamp
					if err != nil {
						log.Fatal(err)
					}
					if (dbResult.Ip != newIfTele.Ip) {
						// Changed detected
						changed = true
					}

					// Update database. This needs to be done always since timestamp should be updated
					err = dbCollection.Update(&dbResult, &newIfTele)
					if err != nil {
						log.Fatalf("Cannot update data to interface table: %v\n", err)
					}
				} else {
					// New interface, changed detected
					changed = true
					err = dbCollection.Insert(&newIfTele)
					if err != nil {
						log.Fatalf("Cannot insert data to interface table: %v\n", err)
					}
				}

				result = append(result, newIfTele)
			}

		}


		// Search for old saved messages
		count, err := dbCollection.Find(bson.M{"timestamp": previousTs}).Count()

		if (count > 0) {
			// Change detected, interface missing
			changed = true

			// Delete all other messages with old timestamp
			err = dbCollection.Remove(bson.M{"timestamp": previousTs})

			if err != nil {
				log.Fatalf("Cannot delete data in interface table: %v\n", err)
			}
		}
		// Send to channel only if there are changes
		if (changed) {
			fmt.Printf("Interface Change Detected -> %v \n\n", result)
			interfaceChannel <- model.TelemetryWrapper{TelMessages: result, TelNode:node.Name, TelType: "interface"}
		}
	}
}

func (node Node) CollectISISData(isisChannel chan model.TelemetryWrapper) {
	// Open database
	session, err := mgo.Dial(os.Getenv("TELEMETRY_DB"))

	if err != nil {
		panic(err)
	}
	defer session.Close()

	// Switch the session to a monotonic behavior.
	session.SetMode(mgo.Monotonic, true)

	dbCollection := session.DB("Telemetry").C("ISIS")

	// Clean database from previous data
	err = dbCollection.Remove(bson.M{})
	// Variable for output formatting
	flag.Parse()

	// Determine the ID for first the transaction.
	var id int64 = 1001

	// Manually specify target parameters.
	router1, err := xr.BuildRouter(
		xr.WithUsername(node.Username),
		xr.WithPassword(node.Password),
		xr.WithHost(node.Ip + ":" + node.Port),
		xr.WithCert(node.CertName),
		xr.WithTimeout(10000),
	)
	if err != nil {
		log.Fatalf("target parameters for router are incorrect: %s", err)
	}

	// Connect to the target
	conn1, ctx1, err := xr.Connect(*router1)
	if err != nil {
		log.Fatalf("could not setup a client connection to %s, %v", router1.Host, err)
	}
	defer conn1.Close()

	id++
	ctx1, cancel := context.WithCancel(ctx1)
	defer cancel()
	c := make(chan os.Signal, 1)

	// If no signals are provided, all incoming signals will be relayed to c.
	// Otherwise, just the provided signals will. E.g.: signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	signal.Notify(c, os.Interrupt)
	defer func() {
		signal.Stop(c)
		cancel()
	}()

	// encoding GPB
	var e int64 = 2

	ch, ech, err := xr.GetSubscription(ctx1, conn1, isisSubscriptionID, id, e)

	if err != nil {
		log.Fatalf("could not setup Telemetry Subscription: %v\n", err)
	}

	go func() {
		select {
		case <-c:
			fmt.Printf("\nmanually cancelled the session to %v\n\n", router1.Host)
			cancel()
			return
		case <-ctx1.Done():
		// Timeout: "context deadline exceeded"
			err = ctx1.Err()
			fmt.Printf("\ngRPC session timed out after %v seconds: %v\n\n", router1.Timeout, err.Error())
			return
		case err = <-ech:
		// Session canceled: "context canceled"
			fmt.Printf("\ngRPC session to %v failed: %v\n\n", router1.Host, err.Error())
			return
		}
	}()

	for tele := range ch {
		result := make([]model.TelemetryMessage, 0)
		message := new(telemetry.Telemetry)
		var previousTs uint64 = 0

		err := proto.Unmarshal(tele, message)
		if err != nil {
			log.Fatalf("could not unmarshall the message: %v\n", err)
		}
		ts := message.GetMsgTimestamp()
		changed := false

		for _, row := range message.GetDataGpb().GetRow() {
			// Get message content
			content := row.GetContent()

			// Parse neighbour telemetry
			nbr := new(isis.IsisShNbr)
			err = proto.Unmarshal(content, nbr)
			if err != nil {
				log.Fatalf("could decode Content: %v\n", err)
			}

			// Get local interface
			lif := nbr.GetLocalInterface()

			// Get first neighbour IP
			if (len(nbr.GetNeighborPerAddressFamilyData()) > 0) {
				if (len(nbr.GetNeighborPerAddressFamilyData()[0].GetIpv4().GetInterfaceAddresses()) > 0) {
					ngrAddrsBytes := nbr.GetNeighborPerAddressFamilyData()[0].GetIpv4().GetInterfaceAddresses()[0]
					ngrAddrsStr := string(ngrAddrsBytes)

					// Create ISISTelemetry struct
					newIsisTelemetry := model.ISISTelemetry{
						LocalInterface:lif,
						NeighbourIp:ngrAddrsStr,
						TimeStamp: ts,
						NodeName:node.Name,
					}
					// Check if exists in database
					dbResult := model.ISISTelemetry{}

					count, err := dbCollection.Find(bson.M{
						"nodename": node.Name,
						"localinterface": lif,
					}).Count()

					if (count > 0) {
						err = dbCollection.Find(bson.M{
							"nodename": node.Name,
							"localinterface":lif,
						}).One(&dbResult)

						// Save the old timestamp
						previousTs = dbResult.TimeStamp

						if err != nil {
							log.Fatal(err)
						}
						if (dbResult.NeighbourIp != newIsisTelemetry.NeighbourIp) {
							// Changed detected
							changed = true
						}

						// Update database. This needs to be done always since timestamp should be updated
						err = dbCollection.Update(&dbResult, &newIsisTelemetry)
						if err != nil {
							log.Fatalf("Cannot update data to ISIS table: %v\n", err)
						}
					} else {
						// New neighbour, changed detected
						changed = true
						err = dbCollection.Insert(&newIsisTelemetry)
						if err != nil {
							log.Fatalf("Cannot insert data to ISIS table: %v\n", err)
						}
					}
					result = append(result, newIsisTelemetry)
				} else {
					count, err := dbCollection.Find(bson.M{"nodename": node.Name}).Count()

					if (count > 0) {
						// Change detected, ISIS neighbour not present anymore
						changed = true

						// Delete all other messages with old timestamp
						err = dbCollection.Remove(bson.M{"nodename": node.Name})

						if err != nil {
							log.Fatalf("Cannot delete data in isis table: %v\n", err)
						}
					}
				}

			} else {
				count, err := dbCollection.Find(bson.M{"nodename": node.Name}).Count()

				if (count > 0) {
					// Change detected, ISIS neighbour not present anymore
					changed = true

					// Delete all other messages with old timestamp
					err = dbCollection.Remove(bson.M{"nodename": node.Name})

					if err != nil {
						log.Fatalf("Cannot delete data in isis table: %v\n", err)
					}
				}
			}

		}

		// Search for old saved messages
		count, err := dbCollection.Find(bson.M{"timestamp": previousTs}).Count()

		if (count > 0) {
			// Change detected, ISIS neighbour not present anymore
			changed = true

			// Delete all other messages with old timestamp
			err = dbCollection.Remove(bson.M{"timestamp": previousTs})

			if err != nil {
				log.Fatalf("Cannot delete data in isis table: %v\n", err)
			}
		}

		// Send to channel only if there are changes
		if (changed) {
			fmt.Printf("ISIS Change Detected -> %v \n\n", result)
			// Send to channel
			isisChannel <- model.TelemetryWrapper{TelMessages: result, TelNode:node.Name, TelType: "isis"}
		}
	}
}

func (node Node) watchForOldData(isisChannel chan model.TelemetryWrapper) {

	// Open database
	session, err := mgo.Dial(os.Getenv("TELEMETRY_DB"))

	if err != nil {
		panic(err)
	}
	defer session.Close()
	lastTs64 := int64(0)
	for {
		changed := false

		// Switch the session to a monotonic behavior.
		session.SetMode(mgo.Monotonic, true)

		dbCollection := session.DB("Telemetry").C("ISIS")
		count, err := dbCollection.Find(bson.M{"nodename": node.Name}).Count()
		if err != nil {
			log.Fatalf("Cannot get data in isis table: %v\n", err)
		}
		if (count > 0) {
			var isisNeighboursDb []model.ISISTelemetry

			// Get all rows
			dbCollection.Find(bson.M{"nodename": node.Name}).All(&isisNeighboursDb)

			for i := range isisNeighboursDb {
				ts64 := int64(isisNeighboursDb[i].TimeStamp * 1000000)

				if (ts64 == lastTs64) {
					fmt.Printf("Old data detected, removing %v", isisNeighboursDb[i].NeighbourIp)
					//If it is older than two seconds remove it from database
					err = dbCollection.Remove(bson.M{"timestamp": isisNeighboursDb[i].TimeStamp})
					if err != nil {
						log.Fatalf("Cannot delete data in isis table: %v\n", err)
					}
					changed = true
					break
				}
			}
			if (len(isisNeighboursDb) > 0) {
				lastTs64 = int64(isisNeighboursDb[0].TimeStamp * 1000000)
			}

		}

		// Send to channel only if there are changes
		if (changed) {
			fmt.Printf("ISIS Change Detected -> %v \n\n", "Old Data Removed")
			// Trigger update to the clients
			isisChannel <- model.TelemetryWrapper{}
		}

		time.Sleep(time.Second * 5)
	}
}