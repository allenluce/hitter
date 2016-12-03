package cluster

import "fmt"

var PERSEC = 100

func Log(format string, args ...interface{}) {
	Clus.SendUI("LOG", fmt.Sprintf(format, args...))
}
