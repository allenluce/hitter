// +build test

package cluster

import (
	"sync"
	"time"

	"github.com/allenluce/faketickers"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/bouk/monkey"
	. "github.com/lyfe-mobile/hitter/common"
	. "github.com/onsi/gomega"
	mgo "gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/mgo.v2/dbtest"
)

func BindPort(c *Cluster) int {
	return int(c.Members.LocalNode().Port)
}

func MemberCountShouldBe(c *Cluster, count int) {
	EventuallyWithOffset(1, c.Count).Should(BeNumerically("==", count))
}

func FakeEC2Metadata(d *ec2metadata.EC2Metadata, s string) (string, error) {
	if s == "/local-ipv4" {
		return "127.0.0.1", nil
	}
	return "i-0testbox0", nil
}

type TestServer struct {
	origPS    int
	Ft        faketickers.FakeTickers
	server    dbtest.DBServer
	DbSession *mgo.Session
	wg        sync.WaitGroup
}

func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false // completed normally
	case <-time.After(timeout):
		return true // timed out
	}
}

func StartTestServer(tempDir string) *TestServer {
	ts := &TestServer{}
	ts.origPS = PERSEC
	PERSEC = 100000
	ts.Ft.Start()
	ts.server.SetPath(tempDir)
	ts.DbSession = ts.server.Session()
	monkey.Patch(mgo.DialWithInfo, func(*mgo.DialInfo) (*mgo.Session, error) { return ts.DbSession, nil })

	campaigns := []string{
		"5817c9b2b1490312be6a8b99",
		"58053cf9b1490349f2d9837f",
		"5807f198b1490316c6c49f59",
		"58080b59b1490316cdbbb2a9",
		"580fc105b149031cf8aa9446",
		"5812561eb149031cf5ebde18",
		"58127270b149031cf5ebde21",
		"581279d3b149031cf5ebde2a",
		"5817d013b1490312bdc49811",
		"5818d518b1490312c2f5f65a",
		"5818dabab1490312c2f5f663",
		"582b2d31b1490312bc97d7bc",
		"582b2f32b1490312c2f5f679",
	}
	// Seed campaigns
	campColl := ts.DbSession.DB(MONGODB[WHICHDB]).C("campaign")
	for _, camp := range campaigns {
		Ω(campColl.Insert(bson.M{"_id": bson.ObjectIdHex(camp)})).Should(Succeed())
	}
	// Seed advertiser
	advColl := ts.DbSession.DB(MONGODB[WHICHDB]).C("advertiser")
	Ω(advColl.Insert(bson.M{"username": "mbolllinger@rhythmone.com", "funds": 20341.0})).Should(Succeed())
	ts.wg.Add(1)
	go func() {
		defer ts.wg.Done()
		HostName = "mainproc"
		//		Main()
	}()
	// Allow main proc's server to start
	Eventually(func() interface{} { return Clus }).ShouldNot(BeNil())
	return ts
}

func (ts *TestServer) Stop() {
	PERSEC = ts.origPS
	ts.Ft.Stop() // Tickers off
	Ω(waitTimeout(&ts.wg, time.Second*5)).Should(BeFalse())
	monkey.UnpatchAll()
	ts.DbSession.Close()
	ts.server.Wipe()
	Clus = nil
}
