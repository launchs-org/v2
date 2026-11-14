package cluster

import (
	"context"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"launchs/worker/src/activity"
)

const (
	// deployIDLabel は、Deployment と Pod に付ける、deploy の ID のラベル。Pod を deploy に結びつけるために使う。
	deployIDLabel = "launchs-org/deploy-id"

	// projectIDLabel は、namespace と Deployment に付ける、プロジェクトの ID のラベル。
	projectIDLabel = "launchs-org/project-id"

	// datadogInjectionLabel は、Datadog の admission webhook が、Pod に監視用の設定を差し込むかを決めるラベル。
	// 差し込まれる設定には hostPath のボリュームが含まれ、PodSecurity(baseline)に拒否されて Pod が作れなくなる。
	// また、利用者のコンテナにホストのパスを見せないためにも、deploy の Pod では無効にする。
	datadogInjectionLabel = "admission.datadoghq.com/enabled"

	// containerName は、deploy の Pod の中の、ユーザーのコンテナの名前。
	containerName = "app"
)

// KubernetesDeploymentOperator は、client-go で、namespace と Deployment を操作する。
type KubernetesDeploymentOperator struct {
	clientset kubernetes.Interface
	// runtimeClassName は、Pod に指定する RuntimeClass。空なら指定せず、クラスタの既定のランタイムを使う。
	runtimeClassName string
}

// NewKubernetesDeploymentOperator は、KubernetesDeploymentOperator を作る。
//
// 引数:
//   - clientset: k8s のクライアント。
//   - runtimeClassName: Pod に指定する RuntimeClass(環境変数 RUNTIME_CLASS)。空なら指定しない。
//
// 戻り値:
//   - 作成した KubernetesDeploymentOperator。
func NewKubernetesDeploymentOperator(clientset kubernetes.Interface, runtimeClassName string) *KubernetesDeploymentOperator {
	// 依存するものを持たせて返す。
	return &KubernetesDeploymentOperator{clientset: clientset, runtimeClassName: runtimeClassName}
}

// EnsureNamespace は、namespace が無ければ作る。すでにあれば何もしない(冪等)。
//
// 作るのは、プロジェクトの ID の形の名前だけ。それ以外は、k8s に触れる前に拒否する。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: namespace の名前(プロジェクトの ID)。
//
// 戻り値:
//   - 名前の形が不正なとき(activity.ErrInvalidNamespaceName)、または k8s への依頼に失敗したときのエラー。
func (operator *KubernetesDeploymentOperator) EnsureNamespace(requestContext context.Context, namespaceName string) error {
	// プロジェクトのものではない名前は、k8s に触れる前に拒否する。
	if validateError := validateProjectNamespace(namespaceName); validateError != nil {
		return validateError
	}

	// namespace を作る。プロジェクトの ID をラベルに付けて、どのプロジェクトのものか分かるようにする。
	_, createError := operator.clientset.CoreV1().Namespaces().Create(requestContext, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespaceName, Labels: map[string]string{projectIDLabel: namespaceName}},
	}, metav1.CreateOptions{})
	// すでにあれば、目的は達成されているので成功とする。
	if apierrors.IsAlreadyExists(createError) {
		return nil
	}
	// それ以外の失敗は、原因を包んで返す(Temporal がリトライする)。
	if createError != nil {
		return fmt.Errorf("namespace %s を作れませんでした: %w", namespaceName, createError)
	}
	return nil
}

// ApplyDeployment は、Deployment を作る。すでにあれば内容を更新する(冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - definition: 作る Deployment の内容。
//
// 戻り値:
//   - namespace の名前の形が不正なとき、または k8s への依頼に失敗したときのエラー。
func (operator *KubernetesDeploymentOperator) ApplyDeployment(requestContext context.Context, definition activity.DeploymentDefinition) error {
	// プロジェクトのものではない namespace には、何も作らない。
	if validateError := validateProjectNamespace(definition.Namespace); validateError != nil {
		return validateError
	}

	// 内容から、k8s の Deployment を組み立てる。
	desiredDeployment := operator.buildDeployment(definition)
	deployments := operator.clientset.AppsV1().Deployments(definition.Namespace)

	// まず作ってみる。
	_, createError := deployments.Create(requestContext, desiredDeployment, metav1.CreateOptions{})
	if createError == nil {
		return nil
	}
	// すでにあるとき以外の失敗は、原因を包んで返す。
	if !apierrors.IsAlreadyExists(createError) {
		return fmt.Errorf("Deployment %s を作れませんでした: %w", definition.Name, createError)
	}

	// すでにあるときは、今の Deployment を読んで、バージョンを引き継いで更新する。
	existingDeployment, getError := deployments.Get(requestContext, definition.Name, metav1.GetOptions{})
	if getError != nil {
		return fmt.Errorf("Deployment %s を取得できませんでした: %w", definition.Name, getError)
	}
	desiredDeployment.ResourceVersion = existingDeployment.ResourceVersion
	if _, updateError := deployments.Update(requestContext, desiredDeployment, metav1.UpdateOptions{}); updateError != nil {
		return fmt.Errorf("Deployment %s を更新できませんでした: %w", definition.Name, updateError)
	}
	return nil
}

// DeploymentReady は、Deployment が、希望するレプリカ数まで起動し終えているかを返す。
//
// Ready とみなす条件(全て満たす):
//   - Deployment の最新の設定を、k8s が処理済み(observedGeneration が generation 以上)
//   - 最新の設定の Pod が、希望する数だけ作られている(updatedReplicas)
//   - 古い設定の Pod が残っていない(replicas が希望する数と同じ)
//   - その全てが、使える状態(availableReplicas)
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: Deployment の namespace(プロジェクトの ID)。
//   - deploymentName: Deployment の名前(deploy の ID)。
//
// 戻り値:
//   - Ready なら true。Deployment がまだ無い場合も、false(作成の直後で、まだ見えないことがあるため)。
//   - k8s に問い合わせられなかったときのエラー。
func (operator *KubernetesDeploymentOperator) DeploymentReady(requestContext context.Context, namespaceName string, deploymentName string) (bool, error) {
	// プロジェクトのものではない namespace は、問い合わせない。
	if validateError := validateProjectNamespace(namespaceName); validateError != nil {
		return false, validateError
	}

	// Deployment を取得する。
	deployment, getError := operator.clientset.AppsV1().Deployments(namespaceName).Get(requestContext, deploymentName, metav1.GetOptions{})
	// まだ無いときは、Ready ではない(作成が見えるようになるのを待つ)。
	if apierrors.IsNotFound(getError) {
		return false, nil
	}
	// それ以外の失敗は、原因を包んで返す。
	if getError != nil {
		return false, fmt.Errorf("Deployment %s を問い合わせられませんでした: %w", deploymentName, getError)
	}

	// 希望するレプリカ数(指定が無ければ、k8s の既定の1)。
	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}
	// 上の条件を、全て満たすかを返す。
	status := deployment.Status
	ready := status.ObservedGeneration >= deployment.Generation &&
		status.UpdatedReplicas == desiredReplicas &&
		status.Replicas == desiredReplicas &&
		status.AvailableReplicas == desiredReplicas
	return ready, nil
}

// buildDeployment は、Deployment の内容から、k8s の Deployment を組み立てる。
//
// 組み立てる内容:
//   - CPU とメモリは、requests と limits を同じ値にする(スペックの大きさ)
//   - 環境変数は、キーの順に並べる(同じ内容なら、毎回同じ Deployment になるように)
//   - command は、指定があれば、コンテナの起動コマンドにする(args も同様)
//   - RuntimeClass は、設定されていれば Pod に指定する
//   - ServiceAccount のトークンを、Pod に渡さない(ユーザーのコンテナに、k8s の権限を渡さないため)
//   - Service 用の環境変数を、Pod に渡さない(他の deploy の情報が漏れないように)
//
// 引数:
//   - definition: 作る Deployment の内容。
//
// 戻り値:
//   - k8s の Deployment。
func (operator *KubernetesDeploymentOperator) buildDeployment(definition activity.DeploymentDefinition) *appsv1.Deployment {
	// Deployment と Pod に付けるラベル。selector と Pod の結びつきに使う。
	labels := map[string]string{
		deployIDLabel:  definition.Name,
		projectIDLabel: definition.Namespace,
	}

	// Pod には、上のラベルに加えて、Datadog の差し込みを無効にするラベルを付ける(selector には使わない)。
	podLabels := map[string]string{datadogInjectionLabel: "false"}
	for labelName, labelValue := range labels {
		podLabels[labelName] = labelValue
	}

	// 環境変数を、キーの順に並べる。
	environmentNames := make([]string, 0, len(definition.Env))
	for environmentName := range definition.Env {
		environmentNames = append(environmentNames, environmentName)
	}
	sort.Strings(environmentNames)
	environment := make([]corev1.EnvVar, 0, len(environmentNames))
	for _, environmentName := range environmentNames {
		environment = append(environment, corev1.EnvVar{Name: environmentName, Value: definition.Env[environmentName]})
	}

	// CPU とメモリの大きさ。requests と limits を同じにする。
	resources := corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewMilliQuantity(int64(definition.CPUMillicores), resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewQuantity(int64(definition.MemoryMiB)*1024*1024, resource.BinarySI),
	}

	// ユーザーのコンテナ。起動コマンドと引数は、指定があるときだけ上書きする。
	container := corev1.Container{
		Name:      containerName,
		Image:     definition.Image,
		Env:       environment,
		Args:      definition.Args,
		Resources: corev1.ResourceRequirements{Requests: resources, Limits: resources},
	}
	if definition.Command != nil {
		container.Command = []string{*definition.Command}
	}

	// Pod の内容。RuntimeClass は、設定されているときだけ指定する。
	podSpec := corev1.PodSpec{
		Containers:                   []corev1.Container{container},
		AutomountServiceAccountToken: boolPointer(false),
		EnableServiceLinks:           boolPointer(false),
	}
	if operator.runtimeClassName != "" {
		podSpec.RuntimeClassName = &operator.runtimeClassName
	}

	// Deployment 全体を組み立てる。
	replicas := definition.Replicas
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: definition.Name, Namespace: definition.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{deployIDLabel: definition.Name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
				Spec:       podSpec,
			},
		},
	}
}

// boolPointer は、bool 値へのポインタを返す(k8s の型が、ポインタで受け取るため)。
//
// 引数:
//   - value: 値。
//
// 戻り値:
//   - 値へのポインタ。
func boolPointer(value bool) *bool {
	// 引数のコピーのアドレスを返す。
	return &value
}
