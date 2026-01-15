package reconcile

type PreviewConfig struct {
	AppName       string
	Namespace     string
	PRNumber      int
	RepoFullName  string
	ImageRef      string
	ContainerPort int32
	RouteHost     string
	RoutePath     string
	HeadSHA       string
	Env           map[string]string
}
