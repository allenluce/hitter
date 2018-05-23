package web_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	. "github.com/allenluce/hitter/cluster"
	"github.com/allenluce/hitter/engine"
	"github.com/allenluce/hitter/web"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/awstesting/mock"
	"github.com/bouk/monkey"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var tempDir string

var _ = BeforeSuite(func() {
	tempDir, _ = ioutil.TempDir("", "hitter"+strconv.Itoa(os.Getpid()))
})

var _ = Describe("Engine", func() {
	var (
		m  *Cluster
		ts *TestServer
	)
	BeforeEach(func() {
		ec2metadata := ec2metadata.New(mock.Session)
		monkey.PatchInstanceMethod(reflect.TypeOf(ec2metadata), "GetMetadata", FakeEC2Metadata)
		ts = StartTestServer(tempDir)
		m = Messenger(Clus)
		go engine.MonitorQPS()
		go web.UI()
	})
	AfterEach(func() {
		m.SendEngine("EXIT")
		m.Stop()  // Messenger off
		ts.Stop() // Server off
		monkey.UnpatchAll()
	})

	FIt("UI receives qps and circularly buffers it", func() {
		for i := 1; i < 109; i++ {
			m.SendUI("QPS", []uint64{uint64(1000 * i), uint64(time.Now().Unix()) * 1000})
		}
		fmt.Printf("During\n")
		// Make sure data ends up in the buffer
		fmt.Printf("During\n")
		Eventually(func() int { return len(Clus.Qps["c1"]) }).Should(Equal(100))
		// Let the channels drain
		time.Sleep(time.Second)
		sum := 0
		for _, qps := range Clus.Qps["c1"] {
			sum += qps.(int)
		}
		Ω(sum).Should(Equal(5850000)) // ∑ 9000...108000
	})
})

// Ginkgo boilerplate, this runs all tests in this package
func TestSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Web Tests")
}
