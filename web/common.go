package web

import (
	"github.com/allenluce/hitter/cluster"
	"github.com/allenluce/hitter/common"
)

func LoadData() interface{} {
	data := struct {
		QPSTarget int
		Procs     int
		WhichDB   string
	}{
		Procs:     cluster.PROCS,
		QPSTarget: cluster.PERSEC,
		WhichDB:   common.WHICHDB,
	}
	return data
}
