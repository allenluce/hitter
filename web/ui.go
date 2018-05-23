package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/allenluce/hitter/cluster"
	"github.com/allenluce/hitter/common"
	"github.com/bradfitz/slice"
	"github.com/braintree/manners"
)

func AmazonHealth(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "OK")
}

func ClusterState(w http.ResponseWriter, r *http.Request) {
	nodes := make([]map[string]interface{}, 0)
	qpssums := make(map[uint64]uint64)
	for _, member := range cluster.Clus.Members.Members() {
		node := cluster.Clus.NewNode(member)
		a := node["qpshistory"].([]interface{})
		for i := 0; i < len(a); i++ {
			switch b := a[i].(type) {
			case []uint64:
				qpssums[b[0]] += b[1]
			case []interface{}:
				ts, err := b[0].(json.Number).Int64()
				if err != nil {
					continue
				}
				qps, err := b[1].(json.Number).Int64()
				if err != nil {
					continue
				}
				qpssums[uint64(ts)] += uint64(qps)
			default:
				fmt.Printf("NO CLUE!!!!!!! %#v\n", a[i])
			}
		}
		nodes = append(nodes, node)
	}
	// Stick QPS sums onto array
	sortedQps := [][]uint64{}
	for ts, v := range qpssums {
		sortedQps = append(sortedQps, []uint64{ts, v})
	}
	slice.Sort(sortedQps, func(i, j int) bool {
		return sortedQps[i][0] < sortedQps[j][0]
	})
	slice.Sort(nodes, func(i, j int) bool {
		return nodes[i]["name"].(string) < nodes[j]["name"].(string)
	})

	response := map[string]interface{}{
		"qpstarget": cluster.PERSEC,
		"numprocs":  cluster.PROCS,
		"nodes":     nodes,
		"qpsdata":   sortedQps,
	}

	b, err := json.Marshal(response)
	if err != nil {
		cluster.Log("Marshaling state JSON: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

func StatsMessageConsumer() {
	for {
		msg, ok := <-cluster.Clus.UIMsgs
		if !ok { // Engine says we're done.
			manners.Close()
			return
		}
		cmds := strings.SplitN(string(msg), " ", 3)
		if len(cmds) < 3 {
			cmds = append(cmds, "")
		}
		cmd, node, value := cmds[0], cmds[1], cmds[2]
		switch cmd {
		case "STARTED":
			cluster.Clus.ConfigMutex.Lock()
			cluster.Clus.States[node] = "play"
			cluster.Clus.ConfigMutex.Unlock()
			message := map[string]interface{}{
				"type": cmd,
				"node": node,
			}
			cluster.WS.WriteJSON(message)
		case "STOPPED":
			cluster.Clus.ConfigMutex.Lock()
			cluster.Clus.States[node] = "stop"
			cluster.Clus.ConfigMutex.Unlock()
			message := map[string]interface{}{
				"type": cmd,
				"node": node,
			}
			cluster.WS.WriteJSON(message)
		case "COLLSTOPPED":
			fallthrough
		case "COLLSTARTED":
			message := map[string]interface{}{
				"type": cmd,
				"node": node,
				"coll": cmds[2],
			}
			cluster.WS.WriteJSON(message)
		case "DBSWITCHED":
			message := map[string]interface{}{
				"type": cmd,
				"db":   value,
			}
			cluster.WS.WriteJSON(message)
		case "LOG":
			cluster.Clus.ConfigMutex.Lock()
			cluster.Clus.Logs.Append(node, value)
			cluster.Clus.ConfigMutex.Unlock()
			fallthrough
		case "TARGETQPSAT":
			fallthrough
		case "PROCSAT":
			message := map[string]interface{}{
				"type":  cmd,
				"node":  node,
				"value": value,
			}
			cluster.WS.WriteJSON(message)
		case "QPS":
			items := strings.Split(cmds[2], " ")
			qps, err := strconv.ParseUint(items[0], 10, 64)
			if err != nil {
				cluster.Log("Error converting qps: %s", err)
				return
			}
			date, err := strconv.ParseUint(items[1], 10, 64)
			if err != nil {
				cluster.Log("Error converting date: %s", err)
				return
			}
			datapoint := []uint64{date, qps} // date, QPS
			cluster.Clus.ConfigMutex.Lock()
			cluster.Clus.Qps.Append(node, datapoint)
			cluster.Clus.ConfigMutex.Unlock()
			// Send to any listeners
			message := map[string]interface{}{
				"type":  "QPS",
				"node":  node,
				"value": datapoint,
			}
			cluster.WS.WriteJSON(message)
		}
	}
}

func rewrite(to string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = to
		handler(w, r)
	}
}

func UI() {
	handler := http.NewServeMux()
	assetHandler := ServeHome()
	handler.HandleFunc("/amazon_health/", AmazonHealth)
	handler.HandleFunc("/state/", ClusterState)
	handler.HandleFunc("/ws", cluster.WS.ServeWs)
	handler.HandleFunc("/favicon.ico", rewrite("assets/ico/favicon.ico", assetHandler))
	handler.HandleFunc("/", assetHandler)

	go StatsMessageConsumer()
	fmt.Printf("Serving on port %d\n", common.WEBPORT)
	err := manners.ListenAndServe(fmt.Sprintf(":%d", common.WEBPORT), handler)
	if err != nil {
		panic(err)
	}
}
