var app;
var comp;

function getIndexByHostname(topology, hostname){
    for(k = 0; k < topology.nodes.length; k++){
        if (topology.nodes[k].hostname == hostname){
            return k;
        }
    }
}

function buildTopology(nx, global, topologyData) {
    $('#topology_container').html('')
    var colorTable = ['#C3A5E4', '#75C6EF', '#CBDA5C', '#ACAEB1 ', '#2CC86F'];
    nx.define('GRPCTopology', nx.ui.Component, {
        view: {
            content: {
                name: 'GRPCTopology',
                type: 'nx.graphic.Topology',
                props: {
                    height: 800,
                    adaptive: true,
                    linkConfig: {
                            linkType: 'curve'
                    },
                    nodeConfig: {
                        label: 'model.name',
                        iconType: "router"
                    },
                     showIcon: true,
                     data: topologyData
                },
                events: {
                    'topologyGenerated': '{#_group}'
                }
            }
        },
        methods: {
            _group: function(sender, event) {
                var groupsLayer = sender.getLayer('groups');
                var nodes1 = [sender.getNode(0), sender.getNode(1)];
                //var group1 = groupsLayer.addGroup({
                //    nodes: nodes1,
                //    label: 'ISIS 49.0000.0162'
                //});
            }
        }
    });
    app = new nx.ui.Application();
    app.container(document.getElementById("topology_container"));
    comp = new GRPCTopology();
    comp.attach(app);
}


var nxData = {
       nodes: [],
       links: []
};

topology = [];

// Web socket to subscribe from the server
var ws = new WebSocket('ws://' + window.location.host + '/ws/topology');

// Start listening
ws.addEventListener('message', function (event) {
    // Parse data
    topology = JSON.parse(event.data);
    updateGraphic(topology);
});


function updateGraphic(pTopology){
    nxData = {
       nodes: [],
       links: []
    };

    // Adding nodes
    for(i = 0; i < topology.length; i++){
        // Graphic layout
        var x=0;
        var y=0;
        if(i == 0){
            x=10
            y=1
        }
        else if(i % 2 == 0){
            x = 20*i;
            y = 10*i;
        }
        else{
            x = 10*i;
            y = 20*i;
        }
        nxData.nodes.push({
            id: i,
            name: topology[i].name,
            "x": x,
            "y": y,
            interfaces: [{name: "1", ipv4:"1.1"},{name:"2",ipv4:"2.2"}]
        });
    }

    // Avoid duplicate links
    processedNodes = [];
    // Adding links
    for(i = 0; i < topology.length; i++){
        for (j = 0; j < topology[i].interfaces.length; j++){
            for (k = 0; k < topology[i].interfaces[j].isisNeighbours.length; k++){

                // Get the neighbour index
                neighbourIndex = getIndexByIp(topology[i].interfaces[j].isisNeighbours[k].ipv4)

                if(neighbourIndex){
                    // If neighbour has been processed already, the link is already in the array
                    if(processedNodes.indexOf(neighbourIndex) == -1){
                            nxData.links.push({
                            source: i,
                            target: neighbourIndex
                        });
                    }

                }
                processedNodes.push(i);
            }
        }
    }

    if(!comp){
        buildTopology(nx, nx.global,nxData);
    }
    else{
        comp._resources.GRPCTopology.data(nxData);
    }

}

function getIndexByIp(ip){
    // Get node name and from that the index

    for (m = 0; m < topology.length; m++){
        for (n = 0; n < topology[m].interfaces.length; n++){
            interfaceIp = topology[m].interfaces[n].ipv4.split("/")[0]
            if(interfaceIp == ip){
                return m;
            }
        }
    }
}
