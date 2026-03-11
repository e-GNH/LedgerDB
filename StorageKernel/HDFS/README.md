Here is the complete, start-to-finish documentation of everything you have built.

# Hadoop Pseudo-Distributed Setup & Go Integration Guide

## 1. Installation & Directory Setup

To run Hadoop locally, you need the pre-compiled binary release rather than the source code.

**Steps:**

1. **Download the compiled binary:**
```bash
cd ~
wget https://downloads.apache.org/hadoop/common/hadoop-3.3.6/hadoop-3.3.6.tar.gz

```


2. **Extract the archive:**
```bash
tar -xzvf hadoop-3.3.6.tar.gz

```


3. **Move to the installation directory:**
```bash
sudo mv hadoop-3.3.6 /usr/local/hadoop

```


4. **Grant ownership to the `hadoop` user:**
This prevents permission denied errors when starting the daemons.
```bash
sudo chown -R hadoop:hadoop /usr/local/hadoop

```


---

## 2. Configuring Pseudo-Distributed Mode

To make Hadoop simulate a full cluster on a single machine, update the XML configuration files located in `/usr/local/hadoop/etc/hadoop/`.

**`core-site.xml`** (Defines the NameNode address):

```xml
<configuration>
    <property>
        <name>fs.defaultFS</name>
        <value>hdfs://localhost:9000</value>
    </property>
</configuration>

```

**`hdfs-site.xml`** (Sets replication to 1 for a single node):

```xml
<configuration>
    <property>
        <name>dfs.replication</name>
        <value>1</value>
    </property>
</configuration>

```

**`mapred-site.xml`** (Sets YARN as the MapReduce framework):

```xml
<configuration>
    <property>
        <name>mapreduce.framework.name</name>
        <value>yarn</value>
    </property>
    <property>
        <name>mapreduce.application.classpath</name>
        <value>$HADOOP_MAPRED_HOME/share/hadoop/mapreduce/*:$HADOOP_MAPRED_HOME/share/hadoop/mapreduce/lib/*</value>
    </property>
</configuration>

```

**`yarn-site.xml`** (Configures the NodeManager):

```xml
<configuration>
    <property>
        <name>yarn.nodemanager.aux-services</name>
        <value>mapreduce_shuffle</value>
    </property>
    <property>
        <name>yarn.nodemanager.env-whitelist</name>
        <value>JAVA_HOME,HADOOP_COMMON_HOME,HADOOP_HDFS_HOME,HADOOP_CONF_DIR,CLASSPATH_PREPEND_DISTCACHE,HADOOP_YARN_HOME,HADOOP_HOME,PATH,LANG,TZ,HADOOP_MAPRED_HOME</value>
    </property>
</configuration>

```

---

## 3. Initializing and Starting Services

Before starting Hadoop for the first time, the NameNode directory must be initialized.

**Steps:**

1. **Format the NameNode (Execute ONLY ONCE):**
```bash
/usr/local/hadoop/bin/hdfs namenode -format
```


2. **Start all daemons (HDFS and YARN):**
```bash
/usr/local/hadoop/sbin/start-all.sh
```


3. **Verify running processes:**
```bash
jps
```


*You should see `NameNode`, `DataNode`, `SecondaryNameNode`, `ResourceManager`, and `NodeManager` listed.*

4. **Stop running processes:**
```bash
/usr/local/hadoop/sbin/stop-all.sh
```
---

## 4. Setting Up the Go Environment

To interact with HDFS programmatically, configure a Go module and download the HDFS client library.

**Steps:**

1. Navigate to your project directory (e.g., `StorageKernel/HDFS`).
2. **Initialize the Go module:**
```bash
go mod init StorageKernel
```


3. **Download the HDFS driver:**
```bash
go get github.com/colinmarc/hdfs/v2
```



*Note on Go Packages: Ensure your script uses `package main` at the very top. This tells the Go compiler that the file is an executable script meant to be run directly from the terminal, rather than a reusable library.*

---

## 5. Go Script for HDFS Connection

This script connects to the local NameNode, creates a file, and writes a specific string to it.

Create `sample_db_connection.go`:

```go
package main

import (
	"fmt"
	"log"

	"github.com/colinmarc/hdfs/v2"
)

func main() {
	// 1. Establish connection to the local NameNode
	client, err := hdfs.New("localhost:9000")
	if err != nil {
		log.Fatalf("Failed to connect to HDFS: %v", err)
	}
	defer client.Close()
	fmt.Println("Connected to HDFS successfully!")

	// 2. Define the path and create the file
	hdfsFilePath := "/test.txt"
	fileWriter, err := client.Create(hdfsFilePath)
	if err != nil {
		log.Fatalf("Failed to create file: %v", err)
	}

	// 3. Write data to the file
	sentence := "testing hadoop\n"
	_, err = fileWriter.Write([]byte(sentence))
	if err != nil {
		log.Fatalf("Failed to write to file: %v", err)
	}
	
	// Flush data and close writer
	fileWriter.Close()
	fmt.Println("Sentence written successfully!")
}

```

**Executing the Script:**
Because the HDFS root directory is owned by the `hadoop` user, bypass permission constraints by injecting the Hadoop username into the environment context during execution:

```bash
HADOOP_USER_NAME=hadoop go run sample_db_connection.go

```

---

## 6. Visualizing Data in the Hadoop GUI

Once the file is written, you can verify it via the built-in web dashboard.

**Steps:**

1. Open your web browser and navigate to the NameNode UI: `http://localhost:9870`.
2. On the top navigation bar, click **Utilities** -> **Browse the file system**.
3. You will see the root directory `/`. Locate `ziad_goals.txt` in the list.
4. Click on the file name to open its details.
5. Click **Head the file** to view the text block and confirm the sentence was successfully stored.
6. Open `http://localhost:8088/cluster/` to view the cluster dashboard.
---

Would you like me to write an additional section for this documentation detailing how to gracefully shut down the cluster and clean up the Go environment?