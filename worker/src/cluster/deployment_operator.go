package cluster

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"launchs/shared/k8s"
	"launchs/worker/src/activity"
)

const (
	// deployIDLabel は、Deployment と Pod に付ける、deploy の ID のラベル。watcher も、このラベルで観測する。
	deployIDLabel = k8s.DeployIDLabel

	// projectIDLabel は、namespace と Deployment に付ける、プロジェクトの ID のラベル。
	projectIDLabel = k8s.ProjectIDLabel

	// datadogInjectionLabel は、Datadog の admission webhook が、Pod に監視用の設定を差し込むかを決めるラベル。
	// 差し込まれる設定には hostPath のボリュームが含まれ、PodSecurity(baseline)に拒否されて Pod が作れなくなる。
	// また、利用者のコンテナにホストのパスを見せないためにも、deploy の Pod では無効にする。
	datadogInjectionLabel = "admission.datadoghq.com/enabled"

	// containerName は、deploy の Pod の中の、ユーザーのコンテナの名前。
	containerName = k8s.AppContainerName

	// filesVolumeName は、deploy のファイル(ConfigMap)を、Pod のボリュームとして付けるときの名前。
	// PVC のボリューム(名前は volume の ID: "volume-…")と、ぶつからない名前にする。
	filesVolumeName = "files"

	// configMapMountVolumePrefix は、共有の ConfigMap のファイルを、Pod のボリュームとして付けるときの名前の接頭辞(後ろに連番を付ける)。
	configMapMountVolumePrefix = "cm"
)

// deploymentNamePattern は、削除してよい Deployment の名前の形("deploy-" + UUID v4)。
var deploymentNamePattern = regexp.MustCompile(`^deploy-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// KubernetesDeploymentOperator は、client-go で、namespace と Deployment を操作する。
type KubernetesDeploymentOperator struct {
	clientset kubernetes.Interface
	// runtimeClassName は、Pod に指定する RuntimeClass。空なら指定せず、クラスタの既定のランタイムを使う。
	runtimeClassName string
	// ingressNamespace は、外からの通信を中継する namespace(Traefik など)。隔離の NetworkPolicy で、ここからの通信を許可する。空なら許可しない。
	ingressNamespace string
	// blockedNetworks は、隔離の NetworkPolicy で、インターネットへの出口の許可から除く、内部のネットワーク(CIDR)。
	blockedNetworks []string
}

// OperatorSettings は、KubernetesDeploymentOperator の設定。
type OperatorSettings struct {
	// RuntimeClass は、Pod に指定する RuntimeClass(環境変数 RUNTIME_CLASS)。空なら指定しない。
	RuntimeClass string

	// IngressNamespace は、外からの通信を中継する namespace(環境変数 INGRESS_NAMESPACE。Traefik など)。空なら許可しない。
	IngressNamespace string

	// BlockedNetworks は、インターネットへの出口の許可から除く、内部のネットワーク(環境変数 EGRESS_BLOCKED_NETWORKS)。
	// 空なら、既定(k8s.DefaultBlockedNetworks)。
	BlockedNetworks []string
}

// NewKubernetesDeploymentOperator は、KubernetesDeploymentOperator を作る。
//
// 引数:
//   - clientset: k8s のクライアント。
//   - settings: 設定(RuntimeClass、外からの通信を中継する namespace、除くネットワーク)。
//
// 戻り値:
//   - 作成した KubernetesDeploymentOperator。
func NewKubernetesDeploymentOperator(clientset kubernetes.Interface, settings OperatorSettings) *KubernetesDeploymentOperator {
	// 依存するものを持たせて返す。
	return &KubernetesDeploymentOperator{
		clientset: clientset, runtimeClassName: settings.RuntimeClass,
		ingressNamespace: settings.IngressNamespace, blockedNetworks: settings.BlockedNetworks,
	}
}

// EnsureNamespace は、namespace が無ければ作り、隔離の NetworkPolicy を適用する。すでにあれば、足りないものだけを揃える(冪等)。
//
// 作るのは、プロジェクトの ID の形の名前だけ。それ以外は、k8s に触れる前に拒否する。
// NetworkPolicy は、namespace の作成のあと、必ず適用する(Cilium は、ポリシーが無いと、全ての通信を許可するため)。
// deploy を作るたびに呼ばれるので、ポリシーが無かった既存のプロジェクトも、次の deploy の作成で、隔離される。
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
	// すでにあるのは、目的が達成されているので、失敗にしない。それ以外の失敗は、原因を包んで返す(Temporal がリトライする)。
	if createError != nil && !apierrors.IsAlreadyExists(createError) {
		return fmt.Errorf("namespace %s を作れませんでした: %w", namespaceName, createError)
	}
	// 隔離の NetworkPolicy を適用する(namespace を作った直後も、すでにあったときも)。
	return operator.ensureIsolationPolicy(requestContext, namespaceName)
}

// ApplyDeployment は、Deployment を作る。すでにあれば内容を更新する(冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - definition: 作る Deployment の内容。
//
// 戻り値:
//   - 更新後の Deployment の generation(設定の世代)。
//   - namespace の名前の形が不正なとき、または k8s への依頼に失敗したときのエラー。
func (operator *KubernetesDeploymentOperator) ApplyDeployment(requestContext context.Context, definition activity.DeploymentDefinition) (int64, error) {
	// プロジェクトのものではない namespace には、何も作らない。
	if validateError := validateProjectNamespace(definition.Namespace); validateError != nil {
		return 0, validateError
	}

	// 内容から、k8s の Deployment を組み立てる。
	desiredDeployment := operator.buildDeployment(definition)
	deployments := operator.clientset.AppsV1().Deployments(definition.Namespace)

	// まず作ってみる。作れたら、その Deployment の世代を返す。
	created, createError := deployments.Create(requestContext, desiredDeployment, metav1.CreateOptions{})
	if createError == nil {
		return created.Generation, nil
	}
	// すでにあるとき以外の失敗は、原因を包んで返す。
	if !apierrors.IsAlreadyExists(createError) {
		return 0, fmt.Errorf("Deployment %s を作れませんでした: %w", definition.Name, createError)
	}

	// すでにあるときは、今の Deployment を読んで、バージョンを引き継いで更新し、更新後の世代を返す。
	existingDeployment, getError := deployments.Get(requestContext, definition.Name, metav1.GetOptions{})
	if getError != nil {
		return 0, fmt.Errorf("Deployment %s を取得できませんでした: %w", definition.Name, getError)
	}
	desiredDeployment.ResourceVersion = existingDeployment.ResourceVersion
	updated, updateError := deployments.Update(requestContext, desiredDeployment, metav1.UpdateOptions{})
	if updateError != nil {
		return 0, fmt.Errorf("Deployment %s を更新できませんでした: %w", definition.Name, updateError)
	}
	return updated.Generation, nil
}

// DeleteDeployment は、Deployment を削除する。Pod は、k8s が後から片付ける(バックグラウンドの削除)。
//
// 無いときは何もしない(冪等)。消すのは、プロジェクトの namespace の中の、deploy の ID の形の名前だけ。
// それ以外は、k8s に触れる前に拒否する(誤って別のものを消さないための安全装置)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: Deployment の namespace(プロジェクトの ID)。
//   - deploymentName: Deployment の名前(deploy の ID)。
//
// 戻り値:
//   - 名前の形が不正なとき(activity.ErrInvalidNamespaceName、activity.ErrInvalidDeploymentName)、
//     または k8s への依頼に失敗したときのエラー。
func (operator *KubernetesDeploymentOperator) DeleteDeployment(requestContext context.Context, namespaceName string, deploymentName string) error {
	// プロジェクトのものではない namespace からは、何も消さない。
	if validateError := validateProjectNamespace(namespaceName); validateError != nil {
		return validateError
	}
	// deploy のものではない名前の Deployment は、消さない。
	if !deploymentNamePattern.MatchString(deploymentName) {
		return fmt.Errorf("%w: %q", activity.ErrInvalidDeploymentName, deploymentName)
	}
	// 子(ReplicaSet、Pod)は、バックグラウンドで片付けさせる。
	propagation := metav1.DeletePropagationBackground
	deleteError := operator.clientset.AppsV1().Deployments(namespaceName).Delete(requestContext, deploymentName, metav1.DeleteOptions{PropagationPolicy: &propagation})
	// すでに無いときは、削除済みとして成功にする。
	if deleteError != nil && !apierrors.IsNotFound(deleteError) {
		return fmt.Errorf("Deployment %s を削除できませんでした: %w", deploymentName, deleteError)
	}
	// deploy のファイル(ConfigMap。名前は deploy の ID)も、一緒に消す(namespace は残るので、ここで消さないと、残ってしまう)。無ければ、何もしない。
	configMapError := operator.clientset.CoreV1().ConfigMaps(namespaceName).Delete(requestContext, deploymentName, metav1.DeleteOptions{})
	if configMapError != nil && !apierrors.IsNotFound(configMapError) {
		return fmt.Errorf("ConfigMap %s を削除できませんでした: %w", deploymentName, configMapError)
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
	// マウントするボリュームがあれば、Pod のボリュームと、コンテナのマウントを足す(PVC の名前は、volume の ID)。
	for _, mount := range definition.Mounts {
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name:         mount.ClaimName,
			VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: mount.ClaimName}},
		})
		podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: mount.ClaimName, MountPath: mount.MountPath})
	}
	// ファイルがあれば、ConfigMap を、1 つのボリュームとして足し、ファイルごとに、subPath でマウントする(ボリュームのマウントと、共存できる)。
	if len(definition.Files) > 0 {
		items := make([]corev1.KeyToPath, 0, len(definition.Files))
		for _, file := range definition.Files {
			items = append(items, corev1.KeyToPath{Key: file.Key, Path: file.Key})
			podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: filesVolumeName, MountPath: file.Path, SubPath: file.Key, ReadOnly: true})
		}
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name:         filesVolumeName,
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: definition.Name}, Items: items, DefaultMode: int32Pointer(0o644)}},
		})
	}
	// 共有の ConfigMap のファイルは、マウントごとに、ボリュームを 1 つ足す(権限 mode は、ボリュームの items に付くため)。subPath でマウントする。
	for index, mount := range definition.ConfigMapMounts {
		volumeName := configMapMountVolumePrefix + strconv.Itoa(index)
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: mount.ConfigMapName},
				Items:                []corev1.KeyToPath{{Key: mount.Key, Path: mount.Key, Mode: int32Pointer(mount.Mode)}},
			}},
		})
		podSpec.Containers[0].VolumeMounts = append(podSpec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: volumeName, MountPath: mount.Path, SubPath: mount.Key, ReadOnly: true})
	}
	if operator.runtimeClassName != "" {
		podSpec.RuntimeClassName = &operator.runtimeClassName
	}

	// Deployment 全体を組み立てる。
	replicas := definition.Replicas
	// ボリュームを持つときは、古い Pod を止めてから、新しい Pod を起動する(RWO の PVC は、古い Pod が持ったままだと、別のノードの新しい Pod が使えないため)。
	strategy := appsv1.DeploymentStrategy{}
	// ファイルを持つときも、同じ方式にする(subPath のマウントは、ConfigMap を更新しても、動いている Pod には反映されない。Pod を作り直して、確実に反映する)。
	if len(definition.Mounts) > 0 || len(definition.Files) > 0 || len(definition.ConfigMapMounts) > 0 {
		strategy.Type = appsv1.RecreateDeploymentStrategyType
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: definition.Name, Namespace: definition.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: strategy,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{deployIDLabel: definition.Name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
				Spec:       podSpec,
			},
		},
	}
}

// int32Pointer は、int32 値へのポインタを返す(k8s の型が、ポインタで受け取るため)。
//
// 引数:
//   - value: 値。
//
// 戻り値:
//   - 値へのポインタ。
func int32Pointer(value int32) *int32 {
	// 引数のコピーのアドレスを返す。
	return &value
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
