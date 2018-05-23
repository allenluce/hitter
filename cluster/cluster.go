package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	. "github.com/allenluce/hitter/common"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/bradfitz/slice"
	"github.com/dustin/go-humanize"
	"github.com/hashicorp/memberlist"
	log "github.com/sirupsen/logrus"
)

var (
	ClusterPort        = 52001
	ClusterRegion      = "us-east-1"
	ClusterAuthProfile = "lyfe"
	Clus               *Cluster
	PROCS              = 1
)

type NodeConfig struct {
	Logs   CircBufMap
	Qps    CircBufMap
	States map[string]string
	Active map[string]map[string]bool
}

type Delegate struct {
	name       string
	enginemsgs *chan []byte
	uimsgs     *chan []byte
	State      string

	cluster *Cluster
}

const (
	ENGINEMESSAGE = iota
	UIMESSAGE
)

func (m *Delegate) NodeMeta(limit int) []byte                  { return nil }
func (m *Delegate) GetBroadcasts(overhead, limit int) [][]byte { return nil }

func (m *Delegate) LocalState(join bool) []byte {
	m.cluster.ConfigMutex.RLock()
	defer m.cluster.ConfigMutex.RUnlock()
	config := NodeConfig{
		Qps:    m.cluster.Qps,
		Logs:   m.cluster.Logs,
		States: m.cluster.States,
		Active: m.cluster.Active,
	}
	b, _ := json.Marshal(config)
	return b
}

func (m *Delegate) MergeRemoteState(s []byte, join bool) {
	mems := make(map[string]bool)
	// Only merge in nodes that are in the member list
	for _, member := range m.cluster.Members.Members() {
		if member.Name == m.cluster.Name { // Ignore info about myself
			continue
		}
		mems[member.Name] = true
	}

	var nodes NodeConfig
	d := json.NewDecoder(bytes.NewReader(s))
	d.UseNumber() // To avoid decimal-to-float conversion
	if err := d.Decode(&nodes); err != nil {
		return
	}
	m.cluster.ConfigMutex.Lock()
	defer m.cluster.ConfigMutex.Unlock()
	for host, _ := range nodes.Qps {
		if _, ok := mems[host]; ok {
			m.cluster.Qps[host] = nodes.Qps[host]
			m.cluster.Logs[host] = nodes.Logs[host]
			m.cluster.States[host] = nodes.States[host]
			m.cluster.Active[host] = nodes.Active[host]
		}
	}
}

func (m *Delegate) NotifyMsg(msg []byte) {
	cp := make([]byte, len(msg)-1)
	copy(cp, msg[1:])
	switch msg[0] {
	case ENGINEMESSAGE:
		*m.enginemsgs <- cp
	case UIMESSAGE:
		*m.uimsgs <- cp
	}
}

type Eventer struct {
	cluster *Cluster
}

func (e *Eventer) NotifyJoin(n *memberlist.Node) {
	// Inform UI of a new member.
	message := map[string]interface{}{
		"type": "NEWNODE",
		"node": e.cluster.NewNode(n),
	}
	WS.WriteJSON(message)
}

func (e *Eventer) NotifyLeave(n *memberlist.Node) {
	e.cluster.ConfigMutex.Lock()
	defer e.cluster.ConfigMutex.Unlock()
	delete(e.cluster.Qps, n.Name)
	delete(e.cluster.Logs, n.Name)
	delete(e.cluster.States, n.Name)
	// Inform UI of a dead member
	message := map[string]interface{}{
		"type": "GONENODE",
		"node": n.Name,
	}
	WS.WriteJSON(message)
}

func (e *Eventer) NotifyUpdate(n *memberlist.Node) {}

type Cluster struct {
	Port        int
	Name        string
	StartTime   time.Time
	Members     *memberlist.Memberlist
	Delegate    *Delegate
	EngineMsgs  chan []byte
	UIMsgs      chan []byte
	Logs        CircBufMap
	Qps         CircBufMap
	States      map[string]string
	ConfigMutex sync.RWMutex
	Active      map[string]map[string]bool
}

var ct int

func NewCluster() *Cluster {
	c := new(Cluster)
	c.Port = ClusterPort // The port I listen on.
	var err error
	c.Name = HostName
	if err != nil {
		panic(err)
	}
	// State data
	c.Qps = NewCircBufMap()
	c.Logs = NewCircBufMap()
	c.States = map[string]string{HostName: "stop"} // Whether each node is started, stopped, or what.
	c.Qps.MakeNode(HostName)                       // Register ourselves
	c.Logs.MakeNode(HostName)                      // Register ourselves

	c.Active = map[string]map[string]bool{
		c.Name: map[string]bool{
			AdvertiserColl: true,
			DeviceColl:     true,
			LocColl:        true,
			CampaignColl:   true,
			TdColl:         true,
		},
	}

	// Message queues
	c.EngineMsgs = make(chan []byte, 100)
	c.UIMsgs = make(chan []byte, 100)
	c.Delegate = &Delegate{
		enginemsgs: &c.EngineMsgs,
		uimsgs:     &c.UIMsgs,
		name:       c.Name,
		State:      fmt.Sprintf("WAYHOOO: %d", ct),
		cluster:    c,
	}
	ct++
	return c
}

// Contact other hitters and create a SWIM cluster
func (c *Cluster) Start() (err error) {
	creds := credentials.NewSharedCredentials("", ClusterAuthProfile)
	svc := ec2metadata.New(session.New(&aws.Config{
		Credentials: creds,
		Region:      aws.String(ClusterRegion),
	}))
	var ipAddr string
	ipAddr, err = svc.GetMetadata("/local-ipv4")
	if err != nil {
		return
	}

	ml := memberlist.DefaultLANConfig()
	ml.Name = c.Name
	ml.BindPort = c.Port
	ml.BindAddr = ipAddr
	ml.Delegate = c.Delegate
	ml.PushPullInterval = time.Second * 5
	ml.LogOutput = ioutil.Discard

	for retries := 50; retries > 0; retries-- {
		c.Members, err = memberlist.Create(ml) // Starts listener
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "address already in use") {
			break
		}
		// Allow old servers to finish up
		time.Sleep(time.Millisecond * 100)
	}
	if err != nil {
		return
	}

	ml.Events = &Eventer{cluster: c}

	others, err := FindEC2Hitters()
	if err != nil {
		return
	}
	if len(others) > 0 {
		if _, err = c.Members.Join(others); err != nil {
			return
		}
	}
	c.StartTime = time.Now()
	return
}

func (c *Cluster) Count() int {
	return c.Members.NumMembers()
}

func (c *Cluster) AmMaster() bool {
	mems := []string{}
	for _, member := range c.Members.Members() {
		mems = append(mems, member.Name)
	}
	sort.Strings(mems)
	if mems[0] == c.Name { // The Lowest Alphabetically
		return true
	}
	return false
}

// Note: sends to everyone, including myself!
// May lead to circular messages if not careful.
func (c *Cluster) send(which int, msg string) {
	codedmsg := append([]byte{byte(which)}, []byte(msg)...)
	for _, member := range c.Members.Members() {
		if member.Name == c.Name { // Not me, the NotifyMsg() below will do that.
			continue
		}
		c.Members.SendToUDP(member, codedmsg)
	}
	c.Delegate.NotifyMsg(codedmsg) // Send to ourselves!
}

func (c *Cluster) SendEngine(msg string) {
	c.send(ENGINEMESSAGE, msg)
}

func (c *Cluster) SendUI(cmd string, msg ...interface{}) {
	var data string
	if len(msg) > 0 {
		switch msg := msg[0].(type) {
		case uint64:
			data = strconv.FormatUint(msg, 10)
		case int:
			data = strconv.Itoa(msg)
		case string:
			data = msg
		case []uint64:
			var datums []string
			for _, datum := range msg {
				datums = append(datums, strconv.FormatUint(datum, 10))
			}
			data = strings.Join(datums, " ")
		default:
			panic(fmt.Sprintf("No clue what this is: %#v (cmd %s)", msg, cmd))
		}
		c.send(UIMESSAGE, fmt.Sprintf("%s %s %s", cmd, c.Name, data))
	} else {
		c.send(UIMESSAGE, fmt.Sprintf("%s %s", cmd, c.Name))
	}
}

func (c *Cluster) Join(host string) error {
	_, err := c.Members.Join([]string{host})
	if err != nil {
		return err
	}
	return nil
}

func (c *Cluster) Stop() {
	c.Members.Leave(time.Second * 5)
	c.Members.Shutdown()
}

// Report to someone hitting /cluster-status
func (c *Cluster) Status(w http.ResponseWriter, r *http.Request) {
	mems := []string{}
	for _, member := range c.Members.Members() {
		mems = append(mems, member.Name)
	}
	status := map[string]interface{}{
		"myHostname":       c.Name,
		"totalMembers":     len(mems),
		"members":          mems,
		"health":           c.Members.GetHealthScore(),
		"serverStart":      c.StartTime,
		"serverStartHuman": humanize.Time(c.StartTime),
	}

	b, err := json.Marshal(status)
	if err != nil {
		log.Errorf("Marshaling status JSON: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
	return
}

func (c *Cluster) NewNode(member *memberlist.Node) map[string]interface{} {
	c.ConfigMutex.Lock()
	defer c.ConfigMutex.Unlock()

	if c.States[member.Name] == "" {
		c.States[member.Name] = "stop"
	}
	colls := []map[string]string{}
	for coll, state := range c.Active[member.Name] {
		collmap := map[string]string{
			"class": "",
			"name":  coll,
			"tag":   strings.ToUpper(string(coll[0])),
		}
		if state {
			collmap["class"] = "btn-success"
		}
		colls = append(colls, collmap)
	}
	slice.Sort(colls, func(i, j int) bool {
		return colls[i]["tag"] < colls[j]["tag"]
	})

	return map[string]interface{}{
		"name":       member.Name,
		"qpshistory": c.Qps[member.Name],
		"logs":       c.Logs[member.Name],
		"targetqps":  PERSEC,
		"procs":      PROCS,
		"state":      c.States[member.Name],
		"colls":      colls,
	}
}

var HostName = GetAWSHostname()

// Get my AWS hostname
func GetAWSHostname() string {
	creds := credentials.NewSharedCredentials("", ClusterAuthProfile)
	svc := ec2metadata.New(session.New(&aws.Config{
		Credentials: creds,
		Region:      aws.String(ClusterRegion),
	}))
	host, err := svc.GetMetadata("/public-hostname")
	if err != nil {
		panic(err)
	}
	return host
}

func FindEC2Hitters() ([]string, error) {
	creds := credentials.NewSharedCredentials("", ClusterAuthProfile)

	svc := ec2.New(session.New(&aws.Config{
		Credentials: creds,
		Region:      aws.String(ClusterRegion),
	}))

	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name: aws.String("instance-state-name"),
				Values: []*string{
					aws.String("running"),
				},
			},
			&ec2.Filter{
				Name: aws.String("tag:Type"),
				Values: []*string{
					aws.String("hitter"),
				},
			},
		},
	}

	resp, err := svc.DescribeInstances(params)
	if err != nil {
		return []string{}, err
	}
	var myVPC string
	// Find my VPC
	for idx, _ := range resp.Reservations {
		for _, inst := range resp.Reservations[idx].Instances {
			if *inst.PublicDnsName == HostName {
				myVPC = *inst.VpcId
			}
		}
	}

	var members []string
	for idx, _ := range resp.Reservations {
		for _, inst := range resp.Reservations[idx].Instances {
			if *inst.PublicDnsName != HostName && *inst.VpcId == myVPC { // Only in my VPC
				members = append(members, fmt.Sprintf("%s:%d", *inst.PrivateIpAddress, ClusterPort))
			}
		}
	}
	return members, nil
}
