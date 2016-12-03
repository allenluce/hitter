// +build !test

// The actual prod data is in bindata.go

package common

var (
	MONGOSTRING = "mongodb://" +
		"svc_rc:EHDgcFeuk7cKCZ0b@rhythmcontent-shard-00-00-ptf9c.mongodb.net," +
		"svc_rc:EHDgcFeuk7cKCZ0b@rhythmcontent-shard-00-01-ptf9c.mongodb.net," +
		"svc_rc:EHDgcFeuk7cKCZ0b@rhythmcontent-shard-00-02-ptf9c.mongodb.net" +
		"?replicaSet=RhythmContent-shard-0"

	MONGODB = "rtbtest"
	WEBPORT = 80
)
