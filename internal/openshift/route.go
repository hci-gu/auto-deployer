package openshift

import "k8s.io/apimachinery/pkg/runtime/schema"

var RouteGVR = schema.GroupVersionResource{
	Group:    "route.openshift.io",
	Version:  "v1",
	Resource: "routes",
}
