package main

import (
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/cmd/kube-scheduler/app"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/defaultbinder"
)

func main() {
	command := app.NewSchedulerCommand(
		app.WithPlugin(defaultbinder.Name, defaultbinder.New),
		app.WithPlugin(deviceAppPluginName, newDevicePlugin),
	)

	if err := command.Execute(); err != nil {
		klog.ErrorS(err, "Error running scheduler")
	}
}
