package cluster_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	. "github.com/allenluce/hitter/cluster"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/awstesting/mock"
	"github.com/bouk/monkey"
	"github.com/hashicorp/memberlist"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

func BeBetween(min, max int) types.GomegaMatcher {
	return SatisfyAll(
		BeNumerically(">=", min),
		BeNumerically("<", max))
}

var hostCount int

var _ = Describe("Clustering", func() {
	Describe("StartCluster", func() {
		var (
			cluster *Cluster
			guard   *monkey.PatchGuard
		)
		BeforeEach(func() {
			ec2metadata := ec2metadata.New(mock.Session)
			guard = monkey.PatchInstanceMethod(reflect.TypeOf(ec2metadata), "GetMetadata", FakeEC2Metadata)
			HostName = "primary"
			cluster = NewCluster()
			cluster.Port = 0
			Ω(cluster.Start()).Should(Succeed())
			MemberCountShouldBe(cluster, 1)
		})
		AfterEach(func() {
			cluster.Stop()
			guard.Unpatch()
		})
		It("runs a cluster", func() {
			// Start two more guys
			HostName = "c1"
			c1 := NewCluster()
			c1.Port = 0
			Ω(c1.Start()).Should(Succeed())
			c1.Join(fmt.Sprintf("127.0.0.1:%d", BindPort(cluster)))
			MemberCountShouldBe(c1, 2)

			HostName = "c2"
			c2 := NewCluster()
			c2.Port = 0
			defer c2.Stop()
			Ω(c2.Start()).Should(Succeed())
			c2.Join(fmt.Sprintf("127.0.0.1:%d", BindPort(cluster)))
			MemberCountShouldBe(c2, 3)

			By("giving status via http")
			w := httptest.NewRecorder()
			u, _ := url.Parse("https://localhost/cluster-status")
			r := &http.Request{Method: "GET", URL: u, RemoteAddr: "192.231.221.82:80"}
			cluster.Status(w, r)
			Ω(w.Code).Should(Equal(http.StatusOK))
			var status map[string]interface{}
			Ω(json.Unmarshal(w.Body.Bytes(), &status)).Should(Succeed())
			Ω(status["myHostname"]).Should(MatchRegexp(`primary`))
			Ω(status["health"]).Should(BeNumerically(">=", 0.0))
			Ω(status["members"]).Should(ConsistOf([]interface{}{
				cluster.Name,
				c1.Name,
				c2.Name,
			}))
			Ω(status["totalMembers"]).Should(Equal(3.0))
			By("surviving the drop of one test node")
			c1.Stop()
			MemberCountShouldBe(cluster, 2)
		})
		It("communicates with one", func() {
			// Start another
			HostName = "c1"
			c1 := NewCluster()
			c1.Port = 0
			defer c1.Stop()
			Ω(c1.Start()).Should(Succeed())
			c1.Join(fmt.Sprintf("127.0.0.1:%d", BindPort(cluster)))
			MemberCountShouldBe(c1, 2)
			msg := "SomeMessage"
			cluster.SendEngine(msg)
			Eventually(c1.EngineMsgs).Should(Receive(Equal([]byte(msg))))
		})
		It("communicates with two", func() {
			HostName = "c1"
			c1 := NewCluster()
			c1.Port = 0
			defer c1.Stop()
			Ω(c1.Start()).Should(Succeed())
			c1.Join(fmt.Sprintf("127.0.0.1:%d", BindPort(cluster)))
			MemberCountShouldBe(c1, 2)
			HostName = "c2"
			c2 := NewCluster()
			c2.Port = 0
			defer c2.Stop()
			Ω(c2.Start()).Should(Succeed())
			c2.Join(fmt.Sprintf("127.0.0.1:%d", BindPort(cluster)))
			MemberCountShouldBe(c2, 3)

			msg := "SomeMessage"
			cluster.SendEngine(msg)
			Eventually(c1.EngineMsgs).Should(Receive(Equal([]byte(msg))))
			Eventually(c2.EngineMsgs).Should(Receive(Equal([]byte(msg))))
		})
		It("communicates with twenty", func() {
			const NUM = 20
			c := make([]*Cluster, NUM)
			for i := 0; i < NUM; i++ {
				HostName = fmt.Sprintf("CTW-%d", i)
				c[i] = NewCluster()
				c[i].Port = 0
				Ω(c[i].Start()).Should(Succeed())
				c[i].Join(fmt.Sprintf("127.0.0.1:%d", BindPort(cluster)))
				MemberCountShouldBe(cluster, i+2)
			}

			msg := "SomeMessage"
			cluster.SendEngine(msg)
			for i := 0; i < NUM; i++ {
				Eventually(c[i].EngineMsgs).Should(Receive(Equal([]byte(msg))), "Host CTW-%d", i)
			}
		})
		It("manages state", func() {
			var createGuard *monkey.PatchGuard
			createGuard = monkey.Patch(memberlist.Create, func(ml *memberlist.Config) (*memberlist.Memberlist, error) {
				defer createGuard.Restore()
				createGuard.Unpatch()
				ml.PushPullInterval = time.Millisecond
				return memberlist.Create(ml)
			})

			c := make([]*Cluster, 5)
			hostnames := []string{"c1", "c2", "c3", "c4", "c5"}
			for i, host := range hostnames {
				HostName = host
				c[i] = NewCluster()
				if i > 0 {
					defer c[i].Stop()
				}
				c[i].Port = 0
				Ω(c[i].Start()).Should(Succeed())
				c[i].Join(fmt.Sprintf("127.0.0.1:%d", BindPort(cluster)))
				MemberCountShouldBe(cluster, i+2)
			}
			time.Sleep(time.Millisecond * 100) // Give it a chance to converge
			hostnames = append(hostnames, "primary")
			for i := 0; i < len(c); i++ {
				var hosts []string
				for hostname, _ := range c[i].Qps {
					hosts = append(hosts, hostname)
				}
				Ω(hosts).Should(ConsistOf(hostnames), "Node %s", c[i].Name)
			}
			By("changing state when a node drops off")
			c[0].Stop()
			time.Sleep(time.Millisecond * 100) // Give it a chance to converge
			hostnames = hostnames[1:]          // Remove c[0]'s hostname from expectation
			for i := 1; i < len(c); i++ {
				var hosts []string
				for hostname, _ := range c[i].Qps {
					hosts = append(hosts, hostname)
				}
				Ω(hosts).Should(ConsistOf(hostnames), "Node %s", c[i].Name)
			}
		})
	})
})

// Ginkgo boilerplate, this runs all tests in this package
func TestSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cluster Tests")
}
