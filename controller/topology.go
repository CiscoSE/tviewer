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
	"html/template"
	"net/http"
	"github.com/gorilla/mux"
	"io/ioutil"
	"fmt"
	"os"
	"github.com/sfloresk/tviewer/model"
	"encoding/json"
	"log"
	"github.com/gorilla/websocket"
	"github.com/go-fsnotify/fsnotify"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type topology struct {
	topologyTemplate *template.Template
	broadcast        chan model.Topology      // broadcast channel
	clients          map[*websocket.Conn]bool // connected clients
	wsUpgrader       websocket.Upgrader
}

func (t topology) registerRoutes(r *mux.Router) {
	r.HandleFunc("/ng/topology", t.handleTemplate)
	r.HandleFunc("/api/topology", t.handleTopology)
	r.HandleFunc("/ws/topology", t.handleWSConnections)
}

func (t topology) handleTemplate(w http.ResponseWriter, r *http.Request) {
	t.topologyTemplate.Execute(w, nil)
}

func (t topology) handleTopology(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		raw, err := ioutil.ReadFile(basePath + "/model/topology.json")
		if err != nil {

			fmt.Println(err.Error())
			os.Exit(1)
		}
		var topology model.Topology
		json.Unmarshal(raw, &topology)
		enc := json.NewEncoder(w)
		enc.Encode(topology)
		t.broadcast <- topology
		break
	default:
		w.WriteHeader(http.StatusBadRequest)
		break
	}
}

func (t topology) handleWSConnections(w http.ResponseWriter, r *http.Request) {
	// Upgrade initial GET request to a websocket
	ws, err := t.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	// Register our new client
	t.clients[ws] = true

	// Trigger information to client
	topology := t.createTopology();
	ws.WriteJSON(topology)
}

func (t topology) sendTopology() {
	for {
		// Grab the next message from the broadcast channel
		topology := <-t.broadcast
		// Send it out to every client that is currently connected
		for client := range t.clients {
			err := client.WriteJSON(topology)
			if err != nil {
				log.Printf("error: %v", err)
				client.Close()
				delete(t.clients, client)
			}
		}
	}
}

func (t topology) watchTopologyChanges(telemetryChannel chan model.TelemetryWrapper) {
	for {
		// Grab any message from the telemetry channel. If a new message arrives, topology has changed
		_ = <-telemetryChannel
		topology := t.createTopology();
		// TODO: Debug
		fmt.Printf("Sending information to clients -> %v \n\n", topology)
		// Send it out to every client that is currently connected
		for client := range t.clients {
			err := client.WriteJSON(topology)
			if err != nil {
				log.Printf("error: %v", err)
				client.Close()
				delete(t.clients, client)
			}
		}
	}

}

func (t topology) WatchTopologyFile() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	done := make(chan bool)
	go func() {
		for {
			select {
			case event := <-watcher.Events:
				log.Println("event:", event)
				if event.Op & fsnotify.Write == fsnotify.Write {
					log.Println("modified file:", event.Name)
					raw, err := ioutil.ReadFile(basePath + "/model/topology.json")
					if err != nil {
						fmt.Println(err.Error())
						os.Exit(1)
					}
					var topology model.Topology
					json.Unmarshal(raw, &topology)
					t.broadcast <- topology
				}
			case err := <-watcher.Errors:
				log.Println("error:", err)
			}
		}
	}()

	err = watcher.Add(basePath + "/model/")
	if err != nil {
		log.Fatal(err)
	}
	<-done
}

func (t topology) createTopology() ([]model.Node) {
	topology := make([]model.Node, 0)
	// Build topology from database

	// Open database
	session, err := mgo.Dial(os.Getenv("TELEMETRY_DB"))
	if err != nil {
		panic(err)
	}
	defer session.Close()

	// Switch the session to a monotonic behavior.
	session.SetMode(mgo.Monotonic, true)

	dbCollection := session.DB("Telemetry").C("Interfaces")

	var interfaces []model.InterfaceTelemetry
	// Get all rows
	dbCollection.Find(bson.M{}).All(&interfaces)


	// Add nodes and interfaces
	for i := range interfaces {
		nodeExists := false
		for j := range topology {
			if (topology[j].Name == interfaces[i].NodeName) {
				nodeExists = true
				// Add interface to node
				topology[j].Interfaces = append(topology[j].Interfaces,
					model.Interface{
						IPv4: interfaces[i].Ip,
						Name: interfaces[i].Interface,
						IsisNeighbours: make([]model.IsisNeighbor, 0),
					})
			}
		}
		if (!nodeExists) {
			// Add interface to node
			newNode := model.Node{
				Name:interfaces[i].NodeName,
				Interfaces: make([]model.Interface, 0),

			}
			topology = append(topology, newNode)
			topology[len(topology) - 1].Interfaces = append(topology[len(topology) - 1].Interfaces,
				model.Interface{
					IPv4: interfaces[i].Ip,
					Name: interfaces[i].Interface,
					IsisNeighbours: make([]model.IsisNeighbor, 0),
				})
		}
	}

	// Add ISIS Neighbours
	var isisNeighboursDb []model.ISISTelemetry

	dbCollection = session.DB("Telemetry").C("ISIS")
	// Get all rows
	dbCollection.Find(bson.M{}).All(&isisNeighboursDb)

	// Add neighbours
	for i := range isisNeighboursDb {
		for j := range topology {
			if (topology[j].Name == isisNeighboursDb[i].NodeName) {
				for k := range topology[j].Interfaces {
					if (isisNeighboursDb[i].LocalInterface == topology[j].Interfaces[k].Name) {
						// Check interface name is equal to the local interface in neighbour
						topology[j].Interfaces[k].IsisNeighbours = append(topology[j].Interfaces[k].IsisNeighbours,
							model.IsisNeighbor{
								IPv4:isisNeighboursDb[i].NeighbourIp,
							})
					}
				}
			}
		}
	}
	return topology

}