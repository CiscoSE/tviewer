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
	"encoding/json"
	"github.com/sfloresk/tviewer/model"
	"log"
	"gopkg.in/mgo.v2"
	"os"
	"gopkg.in/mgo.v2/bson"
	"io/ioutil"
	"flag"
	"bytes"
	xr "github.com/nleiva/xrgrpc"
)

var templ = flag.String("bt", basePath + "/oc-templates/oc-telemetry.json", "Telemetry Config Template")

// NeighborConfig uses asplain notation for AS numbers (RFC5396)
type TelemetryConfig struct {
	SensorGroupID  string
	Path           string
	SubscriptionID string
	SampleInterval int
}

type devices struct {
	devicesTemplate  *template.Template
	telemetryChannel chan model.TelemetryWrapper
}

func (d devices) registerRoutes(r *mux.Router) {
	r.HandleFunc("/ng/devices", d.handleDashboard)
	r.HandleFunc("/api/device", d.handleApiDevice)

}

func (d devices) handleDashboard(w http.ResponseWriter, r *http.Request) {
	d.devicesTemplate.Execute(w, nil)
}

func (d devices) handleApiDevice(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "POST":
		dec := json.NewDecoder(r.Body)
		var device *model.Device = &model.Device{}
		err := dec.Decode(device)

		if err != nil {
			log.Print(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Open database
		session, err := mgo.Dial(os.Getenv("TELEMETRY_DB"))
		if err != nil {
			log.Print("Cannot open database:" + err.Error() + "\n")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		defer session.Close()

		// Switch the session to a monotonic behavior.
		session.SetMode(mgo.Monotonic, true)

		dbCollection := session.DB("Telemetry").C("Devices")

		// Check if the name has been used before
		count, err := dbCollection.Find(bson.M{"name": device.Name}).Count()
		if err != nil {
			log.Print("Cannot read device table:" + err.Error() + "\n")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		if (count > 0) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Name " + device.Name + " already in use"))
			return
		}

		// Check if the ip has been used before
		count, err = dbCollection.Find(bson.M{"ip": device.Ip}).Count()
		if err != nil {
			log.Print("Cannot read device table:" + err.Error() + "\n")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		if (count > 0) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("IP " + device.Ip + " already in use"))
			return
		}

		// Insert new device in Database
		dbCollection.Insert(&device)

		// Create certificate
		content := []byte(device.Certificate)

		err = ioutil.WriteFile(basePath + "/certs/" + device.Name + ".pem", content, 0644)
		if err != nil {
			log.Print("Cannot create cert file:" + err.Error())
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		flag.Parse()

		router, err := xr.BuildRouter(
			xr.WithUsername(device.Username),
			xr.WithPassword(device.Password),
			xr.WithHost(device.Ip + ":" + device.Port),
			xr.WithCert(basePath + "/certs/" + device.Name + ".pem"),
			xr.WithTimeout(15),
		)
		if err != nil {
			log.Print("Target parameters for device are incorrect:" + err.Error() + "\n")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		// Define Telemetry parameters for interfaces
		tConfigInterfaces := &TelemetryConfig{
			SensorGroupID:         ifSensorGroupID,
			Path:          "Cisco-IOS-XR-fib-common-oper:fib/nodes/node/protocols/protocol/vrfs/vrf/interface-infos/interface-info/interfaces/interface",
			SubscriptionID:     ifSubscriptionID,
			SampleInterval: sampleInterval,
		}

		// Define Telemetry parameters for ISIS
		tConfigISIS := &TelemetryConfig{
			SensorGroupID:         isisSensorGroupID,
			Path:          "Cisco-IOS-XR-clns-isis-oper:isis/instances/instance/neighbors/neighbor",
			SubscriptionID:     isisSubscriptionID,
			SampleInterval: sampleInterval,
		}

		// Determine the ID for config.
		var id int64 = 1000

		// Read the OC Telemetry template file
		t, err := template.ParseFiles(*templ)

		// 'buf' is an io.Writter to capture the template execution output for each device
		buf1 := new(bytes.Buffer)
		err = t.Execute(buf1, tConfigInterfaces)
		if err != nil {
			log.Printf("Could not execute interface telemetry config for router: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		// Connect to router
		conn1, ctx1, err := xr.Connect(*router)

		if err != nil {
			log.Printf("could not setup a client connection to %s, %v", router.Host, err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		// Apply the template+parameters to the router.
		_, err = xr.MergeConfig(ctx1, conn1, buf1.String(), id)
		if err != nil {
			log.Fatalf("Failed to config %s: %v\n", router.Host, err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		buf1 = new(bytes.Buffer)
		err = t.Execute(buf1, tConfigISIS)

		_, err = xr.MergeConfig(ctx1, conn1, buf1.String(), id + 1)
		if err != nil {
			log.Printf("failed to config %s: %v\n", router.Host, err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}

		conn1.Close()

		n := Node{}
		n.Ip = device.Ip
		n.CertName = basePath + "/certs/" + device.Name + ".pem"
		n.Name = device.Name
		n.Username = device.Username
		n.Password = device.Password
		n.Port = device.Port

		go n.CollectInterfaceData(d.telemetryChannel)
		go n.CollectISISData(d.telemetryChannel)
		go n.watchForOldData(d.telemetryChannel)

		w.Write([]byte("ok"))
		break
	case "GET":
		var devices []model.Device

		// Open database
		session, err := mgo.Dial(os.Getenv("TELEMETRY_DB"))
		if err != nil {
			log.Fatalf("Cannot open database: %v\n", err)
		}
		defer session.Close()

		// Switch the session to a monotonic behavior.
		session.SetMode(mgo.Monotonic, true)
		dbCollection := session.DB("Telemetry").C("Devices")

		err = dbCollection.Find(nil).All(&devices)
		if err != nil {
			panic(err)
		}
		enc := json.NewEncoder(w)
		enc.Encode(devices)

		break;
	case "DELETE":
		deviceName := r.URL.Query().Get("name")
		// Open database
		session, err := mgo.Dial(os.Getenv("TELEMETRY_DB"))
		if err != nil {
			log.Fatalf("Cannot open database: %v\n", err)
		}
		defer session.Close()

		// Switch the session to a monotonic behavior.
		session.SetMode(mgo.Monotonic, true)
		dbCollection := session.DB("Telemetry").C("Devices")

		err = dbCollection.Remove(bson.M{"name": deviceName})

		if err != nil {
			log.Fatalf("Cannot delete data in device table: %v\n", err)
		}
		w.Write([]byte("ok"))

		break;
	default:
		w.WriteHeader(http.StatusBadRequest)
		break
	}
}