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
)

type index struct {
	indexTemplate *template.Template
}

func (h index) registerRoutes(r *mux.Router) {

	r.HandleFunc("/web/index", h.handleIndex)
	r.HandleFunc("/web/home", h.handleIndex)
	r.HandleFunc("/web/topology", h.handleIndex)
	r.HandleFunc("/web/devices", h.handleIndex)
	r.HandleFunc("/web/", h.handleIndex)
}

func (h index) handleIndex(w http.ResponseWriter, r *http.Request) {
	h.indexTemplate.Execute(w, nil)
}
