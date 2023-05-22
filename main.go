package main

import (
	"github.com/meixiezichuan/device-app-scheduler/networkaware"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/cmd/kube-scheduler/app"
)

func main() {
	command := app.NewSchedulerCommand(
		app.WithPlugin(networkaware.NetworkOverheadName, networkaware.NewNetworkOverhead),
	)

	if err := command.Execute(); err != nil {
		klog.ErrorS(err, "Error running scheduler")
	}
}
