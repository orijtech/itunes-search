# itunes-search
Polyglot search backend cached by Redis and instrumented by OpenCensus end-to-end

![](screenshots/architecture.jpg)

## Prerequisites
* Go1.8+
* Java 7+
* Maven aka `mvn`
* Stackdriver Tracing and Monitoring accounts and GCP credentials
* Redis server

In  your environment, please set `GOOGLE_APPLICATION_CREDENTIALS` to point to
the file containing the Google Cloud Platform credentials for the project in
which you enabled Stackdriver Tracing and Monitoring.

## Server
```shell
go run server.go
```

## Clients

### Go Client
```shell
go run client.go
```

### Java Client
```shell
mvn install && mvn exec:java -Dexec.mainClass=io.mediasearch.search.MediasearchClient
```

## Output
* Tracing

### On cache miss
![](screenshots/cache-miss.png)

### On cache hit
![](screenshots/cache-hit.png)
