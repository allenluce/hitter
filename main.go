package main

import (
	"flag"
	"sync"

	"github.com/allenluce/hitter/cluster"
	"github.com/allenluce/hitter/common"
	"github.com/allenluce/hitter/engine"
	"github.com/allenluce/hitter/web"
)

func Main() {
	if err := engine.DialMongo(); err != nil {
		panic(err)
	}

	clus := cluster.NewCluster()
	err := clus.Start()
	if err != nil {
		panic(err)
	}
	if *clusterhost != "" {
		clus.Join(*clusterhost)
	}
	cluster.Clus = clus
	defer cluster.Clus.Stop()

	go engine.MonitorQPS()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		engine.Engine()
	}()

	go func() {
		defer wg.Done()
		web.UI()
	}()

	wg.Wait()
}

var clusterhost *string

func main() {
	port := flag.Int("port", common.WEBPORT, "Port to listen for web requests")
	clusterport := flag.Int("clusterport", 52001, "Port to listen for cluster")
	clusterhost = flag.String("clusterhost", "", "Connect to this cluster host")
	hn := flag.String("hostname", cluster.HostName, "Name to use for cluster")
	flag.Parse()
	cluster.ClusterPort = *clusterport
	common.WEBPORT = *port
	cluster.HostName = *hn
	Main()
}
