package engine

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lyfe-mobile/hitter/cluster"
	. "github.com/lyfe-mobile/hitter/common"
	"github.com/lyfe-mobile/hitter/data"
	mgo "gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

var LiveDB *mgo.Session
var DBLock sync.RWMutex

type AggFunc func([]string)

const (
	WINS             = "w"
	SPEND            = "sp"
	BIDS             = "b"
	CLICKS           = "c"
	CLIENT_SIDE_LOAD = "csl"
	CITY_KEY         = "ci"
	STATE_KEY        = "s"
	COUNTRY_KEY      = "co"
	CAMPAIGN_KEY     = "cp"
	ADVERTISER_KEY   = "a"
	DATE_KEY         = "d"
	EXCHANGE_KEY     = "e"
	OSV_KEY          = "o"
	HANDSET_KEY      = "h"
	OS_KEY           = "os"
	CARRIER_KEY      = "ca"
)

type Event struct {
	Campaign   string
	Advertiser string
	Date       time.Time
	Site       string
	AdTag      string
	Exchange   int
	AdSize     string
}

func UnpackEncodedID(e string) (c Event, err error) {
	dest, err := base64.URLEncoding.DecodeString(e)
	if err != nil {
		return
	}
	items := strings.Split(string(dest), "&")
	if len(items) < 7 {
		err = fmt.Errorf("Only %d fields in encoded ID, expected 7", len(items))
		return
	}

	c.Date, err = time.Parse(time.RFC3339, items[2])
	if err != nil {
		// Old-style date, parse in our own location
		// At some point this should probably be removed.
		c.Date, err = time.ParseInLocation("2006-01-02 15:04:05", items[2], time.Local)
		if err != nil {
			return
		}
	}
	c.Date = c.Date.UTC()
	c.Campaign = items[0]
	c.Advertiser = items[1]
	c.Site, _ = url.QueryUnescape(items[3])
	c.AdTag, _ = url.QueryUnescape(items[4])
	c.Exchange, _ = strconv.Atoi(items[5])
	c.AdSize = items[6]
	return
}

// AggregateLog takes the name of a collection and an aggregation
// function. It will find all files in the log directory that match
// the collection name with `_static_` and something else added to the end.
//
// It will go through each file and split the lines into 2 or 3
// fields. Those fields are then fed as a string array to the given
// aggregation function.
//
// After it runs through the entire file, it renames the file from
// "static" to "hist"

func AggregateLog(log_path string, coll string, aggFunc AggFunc) {
	log_file, err := data.Asset(log_path)

	if err != nil {
		cluster.Log("Opening log file: %s\n", err.Error())
		return
	}

	scanner := bufio.NewScanner(bytes.NewReader(log_file))
	lineCount := 0
	for scanner.Scan() {
		record := strings.Split(strings.TrimSpace(scanner.Text()), " ")
		aggFunc(record)
		lineCount++
	}
	if err := scanner.Err(); err != nil {
		cluster.Log("Scanner error: %s", err.Error())
		return
	}
}

// makeAggregator returns a function that takes an array of 2 or
// 3 string fields.
//
// The first field is an encoded ID and is used as the aggregation
// key. The second field is the record type (impression, click, etc)

// log_agg is a 2-dimensional map indexed by encodedID and
// record_type.  It contains counts of the number of records of the
// various types seen for a particular encoded ID.
//
// Win records have a third field which is the amount the bid was
// awarded. That's added to a special SPEND field for the given
// encoded ID.

func makeAggregator(log_name string, log_agg map[string]map[string]float64) AggFunc {
	return func(record []string) {
		if len(record) < 2 {
			cluster.Log("Insufficent fields in log record, expected at least 2\n")
			return
		}
		encodedID := record[0]
		record_type := record[1]
		var spend float64
		if record_type == WINS {
			if len(record) < 3 {
				cluster.Log("Insufficient fields in log, expected 3\n")
				return
			}
			var err error
			spend, err = strconv.ParseFloat(record[2], 64)
			if err != nil {
				cluster.Log("Parsing spend: %s", err.Error())
				return
			}
		}
		if _, ok := log_agg[encodedID]; !ok {
			// Only create a new record if we have a complete one to add.
			log_agg[encodedID] = make(map[string]float64)
		}
		if record_type == WINS {
			log_agg[encodedID][SPEND] += spend
		}
		log_agg[encodedID][record_type] += 1
	}
}

// Reconnect to Mongo if we have a recoverable error
// Returns true if it was able to do this.
func tryReconnect(err error, message string) bool {
	if strings.Contains(err.Error(), "i/o timeout") {
		if err = DialMongo(); err != nil {
			cluster.Log("reconnecting to Mongo: %s\n", err.Error())
			return false
		} else {
			cluster.Log("reconnected to Mongo\n")
		}
	} else {
		cluster.Log("%s: %s", message, err)
		return false
	}
	return true
}

func wrapMongo(collName string, f func(*mgo.Collection) (*mgo.ChangeInfo, error)) {
	defer func() {
		if r := recover(); r != nil {
			err, ok := r.(error)
			if !ok {
				panic(r)
			}
			cluster.Log("When upserting/updating: %s", err)
		}
	}()
	for {
		var err error
		DBLock.RLock()
		if LiveDB == nil { // May have been switched
			DBLock.RUnlock()
			if err = DialMongo(); err != nil {
				cluster.Log("switching to Mongo DB %s: %s\n", WHICHDB, err.Error())
				return
			}
			DBLock.RLock()
		}
		theColl := LiveDB.DB(MONGODB[WHICHDB]).C(collName)
		DBLock.RUnlock()
		select {
		case <-perSecTicker.C: // Wait until we can go
			_, err = f(theColl)
			if err == nil {
				atomic.AddUint64(&MyQPS, 1)
				return
			}
			if !tryReconnect(err, fmt.Sprintf("updating %s", collName)) { // failed, try reconnecting.

				return
			}
		case <-tickerChange: // Redo! Ticker changed.
		}
	}
}

func Upsert(collName, encodedID string, set_fields, inc_fields bson.M) {
	wrapMongo(collName, func(theColl *mgo.Collection) (*mgo.ChangeInfo, error) {
		return theColl.Upsert(
			bson.M{"encoded_id": encodedID},
			bson.M{"$set": set_fields, "$inc": inc_fields})
	})
}

func Update(collName string, selector interface{}, update interface{}) {
	wrapMongo(collName, func(theColl *mgo.Collection) (*mgo.ChangeInfo, error) {
		return nil, theColl.Update(selector, update)
	})
}

// CloseChannel doesn't panic when closing closed channels.
func CloseChannel(c chan string) {
	defer func() {
		if r := recover(); r != nil {
			err, ok := r.(error)
			if !ok || (err.Error() != "close of closed channel" &&
				err.Error() != "close of nil channel") {
				panic(r)
			}
		}
	}()
	close(c)
}

func AmDone(coll ...string) (doReturn, abort bool) {
	if !Running { // Abort
		atomic.StoreInt32(&numprocs, int32(0))
		CloseChannel(tasks)
		return true, true // Return and abort
	}
	// Check number of procs
	np := numprocs
	if cluster.PROCS < int(np) { // Too many, we should die.
		if atomic.CompareAndSwapInt32(&numprocs, np, np-1) { // Success.
			if cluster.PROCS == int(np-1) { // Reached the desired number.
				cluster.Clus.SendUI("PROCSAT", int(numprocs))
			}
			return true, true // Return and abort
		}
	}
	if len(coll) > 0 { // Check the collection status
		if !IsActive(coll[0]) { // It's become inactive.
			return true, false // Return, don't abort.
		}
	}
	return
}

func TotalDataLog(log_path string) (abort bool) {
	log_agg := make(map[string]map[string]float64)
	AggregateLog(log_path, TdColl, makeAggregator("totaldata", log_agg))

	for encodedID, update_fields := range log_agg {
		inc_fields := bson.M{
			"impressions_won":  update_fields[WINS],
			CLIENT_SIDE_LOAD:   update_fields[CLIENT_SIDE_LOAD],
			"impressions_seen": update_fields[BIDS],
			"banner_clicks":    update_fields[CLICKS],
			"spend":            update_fields[SPEND],
		}
		td, err := UnpackEncodedID(encodedID)
		if err != nil {
			cluster.Log("Unpacking Encoded TD ID\n")
			continue
		}

		set_fields := bson.M{
			"campaign":   bson.ObjectIdHex(td.Campaign),
			"advertiser": td.Advertiser,
			"date":       td.Date,
			"site":       td.Site,
			"exchange":   td.Exchange,
			"ad_tag":     td.AdTag,
			"ad_size":    td.AdSize,
		}

		var doReturn bool
		if doReturn, abort = AmDone(TdColl); doReturn {
			return
		}
		Upsert(TdColl, encodedID, set_fields, inc_fields)
	}
	return
}

// Takes a string log type which is used as the collection selector.
// record_keys is a list of additional Mongo keys that are expected
//    in the decoded ID for this log type (after the common keys)
func DetailsLog(log_path string, log_type string, record_keys ...string) (abort bool) {
	common_keys := []string{
		CAMPAIGN_KEY,
		ADVERTISER_KEY,
		DATE_KEY,
		EXCHANGE_KEY,
	}

	log_agg := make(map[string]map[string]float64)
	AggregateLog(log_path, log_type, makeAggregator(log_type, log_agg))
	record_keys = append(common_keys, record_keys...)

	for encodedID, update_fields := range log_agg {
		inc_fields := bson.M{
			WINS:             update_fields[WINS],
			CLIENT_SIDE_LOAD: update_fields[CLIENT_SIDE_LOAD],
			BIDS:             update_fields[BIDS],
			CLICKS:           update_fields[CLICKS],
			SPEND:            update_fields[SPEND],
		}

		dest, err := base64.URLEncoding.DecodeString(encodedID)
		if err != nil {
			cluster.Log("Decoding encodedID: %s", err.Error())
			continue
		}
		var items []interface{}
		err = json.Unmarshal(dest, &items)
		if err != nil {
			cluster.Log("unmarshaling details encodedID: %s", err.Error())
			continue
		}

		date, err := time.Parse(time.RFC3339, items[2].(string))
		if err != nil {
			// Old-style date, parse in our own location
			// At some point this should probably be removed.
			date, err = time.ParseInLocation("2006-01-02 15:04:05", items[2].(string), time.Local)
			if err != nil {
				cluster.Log("Parsing time in details log: %s", err.Error())
				continue
			}
		}

		items[2] = date
		set_fields := bson.M{}
		if len(record_keys) > len(items) {
			cluster.Log("Too few fields in log\n")
			continue
		}
		for i, key := range record_keys {
			set_fields[key] = items[i]
		}
		var doReturn bool
		if doReturn, abort = AmDone(log_type); doReturn {
			return
		}
		Upsert(log_type, encodedID, set_fields, inc_fields)
	}
	return
}

func LocLog(log_path string) (abort bool) {
	return DetailsLog(log_path,
		LocColl,
		CITY_KEY,
		STATE_KEY,
		COUNTRY_KEY,
	)
}

func DeviceLog(log_path string) (abort bool) {
	return DetailsLog(log_path,
		DeviceColl,
		HANDSET_KEY,
		OS_KEY,
		OSV_KEY,
		CARRIER_KEY,
	)
}

func AdvertiserLog(log_path string) (abort bool) {
	log_agg := make(map[string]float64)
	AggregateLog(log_path, AdvertiserColl, func(record []string) {
		if len(record) < 2 {
			cluster.Log("Too few fields in advertiser log, expected 2\n")
			return
		}
		advertiser := record[0]
		spend, err := strconv.ParseFloat(record[1], 64)
		if err != nil {
			cluster.Log("error parsing advertiser log spend: %s", err.Error())
			return
		}
		log_agg[advertiser] += spend
	})
	var doReturn bool
	for advertiser, tot_spend := range log_agg {
		if doReturn, abort = AmDone(AdvertiserColl); doReturn {
			return
		}
		Update("advertiser", bson.M{"username": advertiser}, bson.M{"$inc": bson.M{"funds": -tot_spend}})
	}
	return
}

func CampaignLog(log_path string) (abort bool) {
	spends := make(map[string]float64)
	imps := make(map[string]int)
	AggregateLog(log_path, CampaignColl, func(record []string) {
		if len(record) < 2 {
			cluster.Log("Too few fields in campaign log, expected 2\n")
			return
		}
		campaign_id := record[0]
		spend, err := strconv.ParseFloat(record[1], 64)
		if err != nil {
			cluster.Log("error parsing campaign log spend: %s", err.Error())
			return
		}
		spends[campaign_id] += spend
		imps[campaign_id] += 1
	})

	var doReturn bool
	for campaign_id, spend := range spends {
		if doReturn, abort = AmDone(CampaignColl); doReturn {
			return
		}

		Update("campaign",
			bson.M{"_id": bson.ObjectIdHex(campaign_id)}, bson.M{
				"$inc": bson.M{
					"imps":           imps[campaign_id],
					"daily_imps":     imps[campaign_id],
					"spend":          spend,
					"daily_spend":    spend,
					"interval_spend": spend,
					"interval_imps":  imps[campaign_id],
				},
				"$set": bson.M{"last_bid_win": time.Now().UTC()}})
	}
	return
}

// Map of logs to Mongo aggregation functions. Any log that isn't in
// here doesn't get added to Mongo but will be rotated into a hist
// file.

var LogAggregation = map[string]func(string) bool{
	TdColl:         TotalDataLog,
	LocColl:        LocLog,
	DeviceColl:     DeviceLog,
	CampaignColl:   CampaignLog,
	AdvertiserColl: AdvertiserLog,
}

var LogRotation = map[string]time.Duration{
	TdColl:         time.Minute,
	LocColl:        time.Minute * 5,
	DeviceColl:     time.Minute * 5,
	CampaignColl:   time.Second * 10,
	AdvertiserColl: time.Second * 10,
}

const AggLogDir = "logs"

func Glob(pattern string) (matches []string, err error) {
	names := data.AssetNames()
	sort.Strings(names)
	var matched bool
	dir, file := filepath.Split(pattern)

	for _, n := range names {
		n = n[len(dir):]
		matched, err = filepath.Match(file, n)
		if err != nil {
			return
		}
		if matched {
			matches = append(matches, filepath.Join(dir, n))
		}
	}
	return
}

func RunLogs() {
	var alreadyCounted bool
	defer func() {
		if !alreadyCounted { // Don't count this proc on abnormal exit
			atomic.AddInt32(&numprocs, -1)
		}
	}()
	for {
		coll, ok := <-tasks
		if !ok { // Closed channel.
			return
		}
		logPattern := filepath.Join(AggLogDir, coll+"_static_*")
		files, err := Glob(logPattern)
		if err != nil {
			cluster.Log("Globbing files: %s", err.Error())
			return
		}
		if len(files) == 0 { // Nothing to do.
			continue
		}
		for _, log_path := range files { // Go through any existing files
			if doRet, _ := AmDone(); doRet {
				return
			}

			// Call its function if it exists
			if f, ok := LogAggregation[coll]; ok {
				if f(log_path) { // True means abort.
					alreadyCounted = true
					return
				}
			}
		}
	}
}

func DialMongo() error {
	DBLock.Lock()
	defer DBLock.Unlock()
	if LiveDB != nil {
		LiveDB.Close()
		LiveDB = nil
	}

	var session *mgo.Session
	var err error
	if WHICHDB == "newdb" {
		dialInfo, err := mgo.ParseURL(DBS[WHICHDB]) //MONGOSTRING)
		if err != nil {
			panic(err)
		}
		dialInfo.DialServer = func(addr *mgo.ServerAddr) (net.Conn, error) {
			conn, err := tls.Dial("tcp", addr.String(), &tls.Config{
				InsecureSkipVerify: true,
			})
			return conn, err
		}
		dialInfo.Timeout = time.Second
		session, err = mgo.DialWithInfo(dialInfo)
	} else {
		session, err = mgo.Dial(DBS[WHICHDB])
	}

	if err != nil {
		return err
	}
	session.SetSocketTimeout(time.Second * 2)
	session.SetSafe(&mgo.Safe{W: 0})
	session.SetPoolLimit(0)
	DBSESSIONS[WHICHDB] = session
	LiveDB = session
	return nil
}

// Order to run the logs in
var LogOrder = []string{
	AdvertiserColl,
	DeviceColl,
	LocColl,
	CampaignColl,
	TdColl,
}

func EnableColl(coll string) {
	defer cluster.Clus.ConfigMutex.Unlock()
	cluster.Clus.ConfigMutex.Lock()
	cluster.Clus.Active[cluster.HostName][coll] = true
}

func DisableColl(coll string) {
	defer cluster.Clus.ConfigMutex.Unlock()
	cluster.Clus.ConfigMutex.Lock()
	cluster.Clus.Active[cluster.HostName][coll] = false
}

func IsActive(coll string) bool {
	defer cluster.Clus.ConfigMutex.RUnlock()
	cluster.Clus.ConfigMutex.RLock()
	return cluster.Clus.Active[cluster.HostName][coll]
}

// The actual number of goroutines
var numprocs int32

// Queue of collection names to process
var tasks chan string

func AdjustProcs() {
	for cluster.PROCS > int(numprocs) {
		atomic.AddInt32(&numprocs, 1)
		go RunLogs()
		if cluster.PROCS == int(numprocs) {
			cluster.Clus.SendUI("PROCSAT", int(numprocs))
		}
	}
	perSecTicker = time.NewTicker(time.Second / time.Duration(cluster.PERSEC))
	// Tell everyone there's been a change.
	go func(p int32) {
		for i := 0; i < int(p); i++ {
			tickerChange <- true
		}
	}(numprocs)
}

var perSecTicker *time.Ticker
var tickerChange chan bool

// Run n concurrent RunLogs() routines
func MultiRunLogs() {
	defer func() {
		if r := recover(); r != nil {
			err, ok := r.(error)
			if !ok || err.Error() != "send on closed channel" {
				panic(r)
			}
		}
	}()

	CloseChannel(tasks)
	tasks = make(chan string)
	tickerChange = make(chan bool)
	AdjustProcs()
	for {
		for _, coll := range LogOrder {
			if !IsActive(coll) { // Skip ones that aren't active.
				continue
			}
			if !Running { // We're done.
				atomic.StoreInt32(&numprocs, int32(0))
				cluster.Log("Stopping run")
				return
			}
			// Feed 'em tasks.
			tasks <- coll
		}
	}
}
