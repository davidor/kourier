package main

import (
	"courier/pkg/envoy"
	"courier/pkg/knative"
	"courier/pkg/kubernetes"
	log "github.com/sirupsen/logrus"
	"os"
)

const (
	nodeID                = "3scale-courier"
	gatewayPort           = 19001
	managementPort        = 18000
	eventsChannelCapacity = 10
)

func init() {
	// Log as JSON instead of the default ASCII formatter.
	log.SetFormatter(&log.JSONFormatter{})

	// Output to stdout instead of the default stderr
	// Can be any io.Writer, see below for File example
	log.SetOutput(os.Stdout)

	// Only log the warning severity or above.
	log.SetLevel(log.InfoLevel)
}

func main() {
	namespace := ""
	config := kubernetes.Config()
	kubernetesClient := kubernetes.NewKubernetesClient(config)
	knativeClient := knative.NewKnativeClient(config)

	// We observed that sometimes we get many similar events in a small window
	// of time. This buffered channel makes it possible to reload the config
	// only once when there are several events queued.
	eventsChan := make(chan string, eventsChannelCapacity)

	stopChanEndpoints := make(chan struct{})
	go kubernetesClient.WatchChangesInEndpoints(namespace, eventsChan, stopChanEndpoints)

	stopChanServings := make(chan struct{})
	go knativeClient.WatchChangesInServices(namespace, eventsChan, stopChanServings)

	envoyXdsServer := envoy.NewEnvoyXdsServer(gatewayPort, managementPort, kubernetesClient)
	go envoyXdsServer.RunManagementServer()
	go envoyXdsServer.RunGateway()

	for {
		serviceList, err := knativeClient.Services(namespace)
		if err != nil {
			panic(err)
		}

		envoyXdsServer.SetSnapshotForKnativeServices(nodeID, serviceList)

		// If there are no events enqueued, block until there's one.
		// If there are several queued events, consume all to reload the config
		// just once.
		<-eventsChan

		for i := 0; i < len(eventsChan); i++ {
			<-eventsChan
		}

	}
}
