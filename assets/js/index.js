var conn
var nodes = {}
var nodes_by_id = []
var mainchart
var charts = {}
var nodeinfo = []

// Clear out the log counter for this node.
function clearCounter(id) {
  counter = $("#logcounter-" + id )
  counter.text(0)
  counter.toggleClass('hidden', true)
}

// Broadcast this command
function tellEveryone(which) {
  var len = nodes_by_id.length
  for (var i = 0; i < len; i++) {
    if (nodes_by_id[i]) { // Skip deleted ones
      conn.send(which + " " + nodes_by_id[i].name)
    }
  }
}

// Handle per-node play/stop button toggling.
function buttonPlayPress(id) {
  var button = $('#button_play-' + id)
  var cmd = "START"
  if(button.hasClass('btn-success')) { // Playing, send stop.
    cmd = "STOP"
  }
  conn.send(cmd + " " + nodes_by_id[id].name)
}

// Tell all nodes to STOP or START using this collection
function collState(coll, state) {
  var len = nodes_by_id.length
  for (var i = 0; i < len; i++) {
    if (nodes_by_id[i]) { // Skip deleted ones
      conn.send("COLL" + state + " " + nodes_by_id[i].name + " " + coll)
    }
  }
}

// Toggle the state of this collection
function toggleColl(button, host, coll) {
  if($(button).hasClass('btn-success')) { // active, deactivate
    conn.send("COLLSTOP " + host + " " + coll)
  } else {
    conn.send("COLLSTART " + host + " " + coll)
  }
}

// Kills all running nodes.
function die(id) {
  var name = "all"
  if (typeof id !== 'undefined') {
    name = nodes_by_id[id].name
  }
  conn.send("DIE " + name)
}

function changedb(sel) {
  conn.send("DB " + sel.value)
}

// Send the command given plus the value of the input that has the
// command's ID
function sendData(which) {
  conn.send(which + " " + $("#" + which).val())
}

// Add a new node panel.
function NewNode(data, id, reload) {
  data.id = id
  nodes_by_id[id] = data
  // Map of names to ID's
  nodes[data.name] = id
  $.templates("nodeTemplate", {
    markup: "#nodeTemplate"
  })
  // Insert template
  $("#blockList").append(
    $.render.nodeTemplate(data)
  )
  // Update the total count of nodes
  setNodes()
  // Add the chart
  charts[id] = NewChart(data)
  // Add the logs
  if (data.logs) {
    var len = data.logs.length
    for (var i = 0; i < len; i++) {
      insertLog(id, data.logs[i])
    }
  }
  // Zero out external counter
  clearCounter(id)
  // Scroll logs to bottom
  var scroller = $("#logscroll-" + id)
  scroller.scrollTop(scroller.get(0).scrollHeight)
  if (reload) {
    $('.grid').isotope( 'reloadItems' ).isotope()
  }
  $('[data-toggle="tooltip"]').tooltip({delay: { "show": 1000, "hide": 0} , trigger: 'hover'})
}

// Remove a panel for a dead node
function GoneNode(id) {
  $('.grid').isotope( 'remove', $("#node-" + id)).isotope('layout')
  delete nodes_by_id[id] // Don't change indices
  delete nodes[name]
  delete charts[id]
  setNodes()
}

// Get the state of things from the server and add a bunch of nodes at
// once
function AddNodes() {
  $('.grid').isotope({
    itemSelector: '.grid-item', // use a separate class for itemSelector, other than .col-
    masonry: {
      columnWidth: 400,
      gutter: 30
    }
  })
  $.getJSON( "state", function( data ) {
    var len = data.nodes.length
    for (var i = 0; i < len; i++) {
      NewNode(data.nodes[i], i)
    }
    // Set main chart data.
    var qpsdata = normData(data["qpsdata"])
    mainchart.series[0].setData(qpsdata)
    lastTS = qpsdata[qpsdata.length-1][0]
    $('.grid').isotope( 'reloadItems' ).isotope()
  })
}

// Add a log msg to the id'd nodes's log box.
function insertLog(id, msg) {
  var scroller = $("#logscroll-" + id)
  if (scroller && scroller[0]) {
    var atBottom = scroller.scrollTop() + 1 >= scroller[0].scrollHeight - scroller.outerHeight()
    var lastText = $("#logs-" + id + " tbody td span").last().text()
    if (msg == lastText) { // Same, just increment.
      var counter = $("#logs-" + id + " tbody td").last().find('.counter')
      counter.text(Number(counter.text()) + 1) // Increment
      counter.toggleClass('hidden', false)
    } else {
      msg = '<span class="counter hidden">1</span><span>' + msg + '</span>'
      $("#logs-" + id + " tbody").append("<tr><td>" + msg + "</td></tr>")
      if (atBottom) { // Keep on the bottom.
        scroller.scrollTop(scroller.get(0).scrollHeight)
      }
    }
    // If log panel isn't open, increment its counter.
    if (!$('#logs-' + id).is(':visible')) {
      counter = $("#logcounter-" + id )
      counter.text(Number(counter.text()) + 1) // Increment
      counter.toggleClass('hidden', false)
    }
  }
}


var QPSdata = {}

// Every second see if we have another complete datapoint to add to
// the UI and main chart.

var lastTS = 0 // Last timestamp we added.

function checkQPSData() {
  for (var ts in QPSdata) {
    if (ts < lastTS) {
      delete QPSdata[ts] // Old, ditch it.
    } else {
      if (QPSdata[ts].count == nodeCount()) { // Got a full one!
        mainchart.addPoint([Number(ts), QPSdata[ts].sum])
        $('#qpstotal bold').text(QPSdata[ts].sum) // UPdate UI
        lastTS = ts
        delete QPSdata[ts]
      }
    }
  }
  setTimeout(checkQPSData, 1000)
}


function nodeCount() {
  var count = 0
  var len = nodes_by_id.length
  for (var i = 0; i < len; i++) {
    if (nodes_by_id[i]) { // Skip already deleted ones
      count++
    }
  }
  return count
}

function setNodes() {
  $("#nodecount").text("Nodes: " + nodeCount())
}

// Derive the proper URL for the websocket connection.
function wsUrl() {
  var l = window.location
  return ((l.protocol === "https:") ? "wss://" : "ws://") + l.host + '/ws'
}

function SetupWebsocket () {
  $('#recon').toggleClass('hidden', true)
  conn = new WebSocket(wsUrl())
  conn.onclose = function(evt) {
    $('#recon').toggleClass('hidden', false)
    // Attempt to reconnect
    setTimeout(SetupWebsocket, 1000)
  }
  conn.onopen = function(evt) {
    // Good connection.
    // Remove any existing nodes.
    var len = nodes_by_id.length
    for (var i = 0; i < len; i++) {
      if (nodes_by_id[i]) { // Skip already deleted ones
        GoneNode(i)
      }
    }
    // Zero out containers
    nodes = {}
    nodes_by_id = []
    charts = {}
    // Add the nodes!
    if (len > 0) { // After Isotope finishes rendering
      var total = 0
      $('.grid').on(
        'removeComplete',
        function( event, removedItems ) {
          total += removedItems.length
          if (total >= len) {
            $('.grid').off('removeComplete')
            AddNodes()
          }
        }
      )
    } else { // Right now.
      AddNodes()
    }
  }
  conn.onmessage = function(evt) {
    var msg = JSON.parse(evt.data)
    var id = nodes[msg.node]
    switch (msg.type) {
    case 'NEWNODE':
      NewNode(msg.node, nodes_by_id.length, true)
      break
    case 'GONENODE':
      GoneNode(id)
      break
    case 'TARGETQPSAT':
      $("#targetqps-" + id).text("target " + msg.value)
      break
    case 'PROCSAT':
      $("#procs-" + id).text("procs " + msg.value)
      break
    case 'QPS':
      if (charts[id]) {
        charts[id].addPoint(msg.value)
      }
      $("#qps-" + id).text(msg.value[1])
      if (!nodeinfo[id]) {
        nodeinfo[id] = {}
      }
      nodeinfo[id].qps = msg.value
      if (!QPSdata[msg.value[0]]) {
        QPSdata[msg.value[0]] = {count: 0, sum:0}
      }
      QPSdata[msg.value[0]].sum += msg.value[1]
      QPSdata[msg.value[0]].count++
      break
    case 'STARTED':
      var button = $('#button_play-' + nodes[msg.node])
      button.toggleClass('btn-success', true)
      button.find("i").attr('class', "fa fa-stop")
      break
    case 'STOPPED':
      var button = $('#button_play-' + nodes[msg.node])
      button.toggleClass('btn-success', false)
      button.find("i").attr('class', "fa fa-play")
      break
    case 'COLLSTARTED':
      var button = $('#coll' + msg.coll + '-' + nodes[msg.node])
      button.toggleClass('btn-success', true)
      break
    case 'COLLSTOPPED':
      var button = $('#coll' + msg.coll + '-' + nodes[msg.node])
      button.toggleClass('btn-success', false)
      break
    case 'LOG':
      insertLog(id, msg.value)
      break
    case 'DBSWITCHED':
      $("select").val(msg.db)
      break
    }
    return true
  }
}

$(document).ready(function() {
  if (window["WebSocket"]) {
    SetupWebsocket()
    // Put collection buttons on the control panel.
    $.templates("collTemplate", {
      markup: "#collTemplate"
    })
    var colls = {
      A: 'advertiser',
	    C: 'campaign',
	    D: 'device_data',
	    L: 'location_data',
	    T: 'total_data',
    }
    $("#collbuttons").append(
      $.render.collTemplate(colls)
    )
    // Add the main chart
    mainchart = NewChart({
      id: "main",
      qpshistory: []
    }, 350)
    setTimeout(checkQPSData, 1000)
  } else {
    alert("Your browser does not support WebSockets.")
  }
})
