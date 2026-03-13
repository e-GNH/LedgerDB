package main

import (
	"github.com/redis/go-redis/v9"
	"github.com/colinmarc/hdfs/v2"
	
)

func newRedisClient() *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password
		DB:       0,  // use default DB
		Protocol: 2,
	})
	return rdb
}


func newHDFSClient() (*hdfs.Client, error) {
	client, err := hdfs.New("localhost:9000")
	if err != nil {
		return nil, err
	}
	return client, nil
}