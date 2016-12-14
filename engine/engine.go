package engine

import (
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lyfe-mobile/hitter/cluster"
	. "github.com/lyfe-mobile/hitter/common"
)

var (
	Running bool
	MyQPS   uint64
)

func Ticking(ticker *time.Ticker) bool {
	_, ok := <-ticker.C
	return ok
}

func MonitorQPS() {
	ticker := time.NewTicker(time.Second)
	for Ticking(ticker) {
		qps := atomic.LoadUint64(&MyQPS)
		atomic.StoreUint64(&MyQPS, 0)
		cluster.Clus.SendUI("QPS", []uint64{qps, uint64(time.Now().Unix()) * 1000})
	}
}

var LetterToColl = map[string]string{
	"A": AdvertiserColl,
	"D": DeviceColl,
	"L": LocColl,
	"C": CampaignColl,
	"T": TdColl,
}

func Engine() {
	atomic.StoreUint64(&MyQPS, 0)

	for {
		var delay <-chan time.Time
		if Running {
			delay = time.After(time.Millisecond * 100)
		} else {
			delay = time.After(time.Millisecond * 0)
		}
		select {
		case msg, ok := <-cluster.Clus.EngineMsgs:
			if !ok { // UI says we're done.
				return
			}
			cmds := strings.Split(string(msg), " ")
			switch cmds[0] {
			case "START":
				if cmds[1] != cluster.HostName || Running {
					break
				}
				Running = true
				cluster.Clus.SendUI("STARTED")
				go func() {
					for {
						if !Running {
							return
						}
						MultiRunLogs()
					}
				}()
			case "STOP":
				if cmds[1] != cluster.HostName || !Running {
					break
				}
				Running = false
				cluster.Clus.SendUI("STOPPED")
			case "ONCE": // For testing
				if Running {
					break
				}
				Running = true
				MultiRunLogs()
				Running = false
				cluster.Clus.SendEngine("DONE")
			case "PROCS":
				cluster.PROCS, _ = strconv.Atoi(cmds[1])
				if Running {
					AdjustProcs()
				} else {
					cluster.Clus.SendUI("PROCSAT", cluster.PROCS)
				}
			case "EXIT":
				close(cluster.Clus.UIMsgs) // Tell UI we're done.
				return
			case "TARGETQPS":
				cluster.PERSEC, _ = strconv.Atoi(cmds[1])
				if Running {
					AdjustProcs()
				}
				cluster.Clus.SendUI("TARGETQPSAT", cluster.PERSEC)
			case "COLLSTART":
				if cmds[1] == cluster.HostName {
					EnableColl(LetterToColl[cmds[2]])
					cluster.Clus.SendUI("COLLSTARTED", cmds[2])
					// Tell everyone there's been a change.
					go func(p int32) {
						for i := 0; i < int(p); i++ {
							tickerChange <- true
						}
					}(numprocs)
				}
			case "COLLSTOP":
				if cmds[1] == cluster.HostName {
					DisableColl(LetterToColl[cmds[2]])
					cluster.Clus.SendUI("COLLSTOPPED", cmds[2])
					// Tell everyone there's been a change.
					go func(p int32) {
						for i := 0; i < int(p); i++ {
							tickerChange <- true
						}
					}(numprocs)
				}
			case "DB":
				WHICHDB = cmds[1]
				DBLock.Lock()
				LiveDB = nil
				DBLock.Unlock()
				cluster.Clus.SendUI("DBSWITCHED", cmds[1])
			case "DIE": // We're outta here!
				if cmds[1] == cluster.HostName || cmds[1] == "all" {
					Running = false
					time.Sleep(time.Millisecond * 500) // Wait for everyone to get the message
					cluster.Clus.Stop()
					os.Exit(1)
				}
			}
		case <-delay:
			break
		}
	}
}
