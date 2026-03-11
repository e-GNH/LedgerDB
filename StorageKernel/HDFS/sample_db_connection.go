package main

import (
	// "context"
	"fmt"
	"log"
	"os"

	"github.com/colinmarc/hdfs/v2"
)

func createFile(client *hdfs.Client, hdfsFilePath string) (*hdfs.FileWriter, error) {
	_, err := client.Stat(hdfsFilePath)

	if err == nil {
		fmt.Printf("File %s exists on HDFS\n", hdfsFilePath)
		// Open for append when file exists
		writer, err := client.Append(hdfsFilePath)
		if err != nil {
			return nil, err
		}
		fmt.Printf("File %s opened for append on HDFS\n", hdfsFilePath)
		return writer, nil
	} else if os.IsNotExist(err) {
		fmt.Printf("File %s does not exist on HDFS\n", hdfsFilePath)
		writer, err := client.Create(hdfsFilePath)
		if err != nil {
			return nil, err
		}
		fmt.Printf("File %s created successfully on HDFS\n", hdfsFilePath)
		return writer, nil
	} else {
		fmt.Printf("Error checking file existence: %v\n", err)
		return nil, err
	}
}

func main() {
	// 1. Establish the connection to HDFS
	client, err := hdfs.New("localhost:9000")
	if err != nil {
		log.Fatalf("Failed to connect to HDFS: %v", err)
	}
	defer client.Close()
	fmt.Println("Connected to HDFS successfully!")
	// 2. Define the path and create the file
	hdfsFilePath := "/test.txt"

	fileWriter, err := createFile(client, hdfsFilePath)
	if err != nil {
		log.Fatalf("Error creating/opening file on HDFS: %v", err)
		return
	}
	defer fileWriter.Close()

	fmt.Printf("File %s is ready for writing\n", hdfsFilePath)

	// 3. Write the exact sentence
	sentence := "testing hadoop\n"
	bytesWritten, err := fileWriter.Write([]byte(sentence))
	if err != nil {
		log.Fatalf("Failed to write to file: %v", err)
	}

	fmt.Printf("Successfully wrote %d bytes to %s\n", bytesWritten, hdfsFilePath)
}
