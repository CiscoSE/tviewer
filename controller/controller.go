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
	"github.com/gorilla/websocket"
	"github.com/sfloresk/tviewer/model"
	"gopkg.in/mgo.v2"
	"os"
	"log"
)

const basePath = "src/github.com/sfloresk/tviewer"
const ifSubscriptionID = "tviewerIFCS"
const isisSubscriptionID = "tviewerISIS"
const ifSensorGroupID = "tviewerInterfaces"
const isisSensorGroupID = "tviewerISISNeighbor"
const sampleInterval = 2000

var (
	indexController index
	homeController home
	topologyController topology
	devicesController devices
)

func Startup(templates map[string]*template.Template, r *mux.Router) {
	// Create cert directory if doesn't exist

	_ = os.Mkdir(basePath + "/certs", os.ModePerm)

	// Create the channel
	telemetryChan := make(chan model.TelemetryWrapper)

	indexController.indexTemplate = templates["index.html"]
	indexController.registerRoutes(r)

	homeController.homeTemplate = templates["home.html"]
	homeController.registerRoutes(r)

	devicesController.devicesTemplate = templates["devices.html"]
	devicesController.telemetryChannel = telemetryChan
	devicesController.registerRoutes(r)

	topologyController.topologyTemplate = templates["topology.html"]
	topologyController.clients = make(map[*websocket.Conn]bool)
	topologyController.broadcast = make(chan model.Topology)
	topologyController.wsUpgrader = websocket.Upgrader{}
	topologyController.registerRoutes(r)

	// Start telemetry of devices that are in the database
	// Open database
	session, err := mgo.Dial(os.Getenv("TELEMETRY_DB"))
	if err != nil {
		log.Fatal("Cannot open database:" + err.Error() + "\n")
	}
	defer session.Close()

	// Switch the session to a monotonic behavior.
	session.SetMode(mgo.Monotonic, true)

	dbCollection := session.DB("Telemetry").C("Devices")

	var devices []model.Device

	err = dbCollection.Find(nil).All(&devices)
	if err != nil {
		log.Fatal("Cannot read devices table:" + err.Error() + "\n")
	}
	for _, device := range devices {
		n := Node{}
		n.Ip = device.Ip
		n.CertName = basePath + "/certs/" + device.Name + ".pem"
		n.Name = device.Name
		n.Username = device.Username
		n.Password = device.Password
		n.Port = device.Port

		go n.CollectInterfaceData(telemetryChan)
		go n.CollectISISData(telemetryChan)
		go n.watchForOldData(telemetryChan)
	}


	// Start listening for collection
	go topologyController.watchTopologyChanges(telemetryChan)

	r.PathPrefix("/").Handler(http.FileServer(http.Dir(basePath + "/public")))

}

