package engine_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strconv"
	"testing"

	"gopkg.in/mgo.v2/bson"

	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/awstesting/mock"
	"github.com/bouk/monkey"
	. "github.com/lyfe-mobile/hitter/cluster"
	. "github.com/lyfe-mobile/hitter/common"
	. "github.com/lyfe-mobile/hitter/engine"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var tempDir string

var _ = BeforeSuite(func() {
	tempDir, _ = ioutil.TempDir("", "hitter"+strconv.Itoa(os.Getpid()))
})

var _ = AfterSuite(func() {
	os.RemoveAll(tempDir)
})

var _ = Describe("Engine", func() {
	BeforeEach(func() {
		ec2metadata := ec2metadata.New(mock.Session)
		monkey.PatchInstanceMethod(reflect.TypeOf(ec2metadata), "GetMetadata", FakeEC2Metadata)
	})
	AfterEach(func() {
		monkey.UnpatchAll()
	})
	It("can connnect to Mongo", func() {
		Ω(DialMongo()).Should(Succeed())
		Ω(LiveDB.DB(MONGODB[WHICHDB]).CollectionNames()).Should(ContainElement("keymap2_rhythmx"))
		LiveDB.Close()
	})
	It("will glob embedded filenames", func() {
		Ω(Glob("logs/device_*13:06*")).Should(Equal([]string{"logs/device_data_static_2016-11-16T13:06:25Z"}))
	})
	Describe("Data loads", func() {
		var (
			m  *Cluster
			ts *TestServer
		)
		BeforeEach(func() {
			ts = StartTestServer(tempDir)
			m = Messenger(Clus)
		})
		AfterEach(func() {
			m.SendEngine("EXIT")
			m.Stop()  // Messenger off
			ts.Stop() // Server off
		})
		It("loads data locally", func() {
			m.SendEngine("ONCE")
			Eventually(m.EngineMsgs, 3).Should(Receive(Equal([]byte("DONE"))))

			tdColl := ts.DbSession.DB(MONGODB[WHICHDB]).C("total_data")
			query := tdColl.Find(bson.M{})
			Ω(tdColl.Count()).Should(BeNumerically("==", 109))
			var results []interface{}
			Ω(query.Iter().All(&results)).Should(Succeed())
			var won float64
			for _, inter := range results {
				result := inter.(bson.M)
				Ω(result["advertiser"]).Should(Equal("mbolllinger@rhythmone.com"))
				won += result["impressions_won"].(float64)
			}
			Ω(won).Should(BeNumerically("==", 469))

			devColl := ts.DbSession.DB(MONGODB[WHICHDB]).C("device_data")
			Ω(devColl.Count()).Should(BeNumerically("==", 398))

			locColl := ts.DbSession.DB(MONGODB[WHICHDB]).C("location_data")
			Ω(locColl.Count()).Should(BeNumerically("==", 979))

			advColl := ts.DbSession.DB(MONGODB[WHICHDB]).C("advertiser")
			query = advColl.Find(bson.M{})
			Ω(query.Iter().All(&results)).Should(Succeed())
			for _, inter := range results {
				result := inter.(bson.M)
				Ω(result["funds"]).Should(BeNumerically("==", 1))
			}

			campColl := ts.DbSession.DB(MONGODB[WHICHDB]).C("campaign")
			query = campColl.Find(bson.M{})
			Ω(query.Iter().All(&results)).Should(Succeed())
			for _, inter := range results {
				result := inter.(bson.M)
				Ω(result["imps"]).Should(BeNumerically(">=", 1))
			}
			ts.Ft.Tick() // Get qpsmonitor going.
			Eventually(m.UIMsgs).Should(Receive(Equal([]uint8("QPS c1 1500"))))
		})
		It("can load with multiple procs", func() {
			m.SendEngine("PROCS 3")
			m.SendEngine("ONCE")
			Eventually(m.EngineMsgs, 3).Should(Receive(Equal([]byte("DONE"))))

			tdColl := ts.DbSession.DB(MONGODB[WHICHDB]).C("total_data")
			query := tdColl.Find(bson.M{})
			Ω(tdColl.Count()).Should(BeNumerically("==", 109))
			var results []interface{}
			Ω(query.Iter().All(&results)).Should(Succeed())
			var won float64
			for _, inter := range results {
				result := inter.(bson.M)
				won += result["impressions_won"].(float64)
			}
			Ω(won).Should(BeNumerically("==", 469*3))
		})
		It("logs errors", func() {
			monkey.Patch(Glob, func(string) ([]string, error) {
				return nil, fmt.Errorf("Fake Globbing Error")
			})
			m.SendEngine("ONCE")
			Eventually(m.EngineMsgs).Should(Receive(Equal([]byte("DONE"))))
			Eventually(m.UIMsgs).Should(Receive(MatchRegexp("LOG c1 .* Globbing files: Fake Globbing Error")))
		})
		It("UI receives logs", func() {
			m.SendUI("LOG", "Some Log Message")
			Eventually(func() int { return len(Clus.Logs["c1"]) }).Should(Equal(1))
			Ω(Clus.Logs["c1"]).Should(Equal([]interface{}{"Some Log Message"}))
		})
	})
})

// Ginkgo boilerplate, this runs all tests in this package
func TestSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Engine Tests")
}
