package web

import (
	"github.com/lyfe-mobile/hitter/cluster"
	"github.com/lyfe-mobile/hitter/common"
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
