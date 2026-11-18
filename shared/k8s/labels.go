package k8s

const (
	// DeployIDLabel は、Deployment と Pod に付ける、deploy の ID のラベル。Pod を deploy に結びつけるために使う。
	// ワーカーが付け、watcher が、このラベルのあるものだけを観測する。
	DeployIDLabel = "launchs-org/deploy-id"

	// ProjectIDLabel は、namespace と Deployment に付ける、プロジェクトの ID のラベル。
	ProjectIDLabel = "launchs-org/project-id"

	// AppContainerName は、deploy の Pod の中の、ユーザーのコンテナの名前。
	AppContainerName = "app"
)
