package cluster

import (
	"context"
	"fmt"
	"regexp"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"launchs/worker/src/activity"
)

// volumeNamePattern は、PVC の名前として受け付ける形(volume の ID: "volume-" + UUID v4)。
var volumeNamePattern = regexp.MustCompile(`^volume-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

const (
	// defaultExpandPollInterval は、PVC の拡張が反映されたかを確かめる間隔。
	defaultExpandPollInterval = 2 * time.Second

	// defaultExpandTimeout は、PVC の拡張が反映されるのを待つ最長の時間。
	defaultExpandTimeout = 5 * time.Minute
)

// VolumeSettings は、KubernetesVolumeOperator の設定。
type VolumeSettings struct {
	// StorageClass は、PVC に指定する StorageClass(環境変数 VOLUME_STORAGE_CLASS)。空なら指定せず、クラスターの既定を使う。
	StorageClass string

	// AccessMode は、PVC のアクセスモード(環境変数 VOLUME_ACCESS_MODE。ReadWriteOnce または ReadWriteMany)。
	AccessMode string
}

// KubernetesVolumeOperator は、client-go で PVC を操作する VolumeOperator の実装。
type KubernetesVolumeOperator struct {
	// clientset は、k8s のクライアント。
	clientset kubernetes.Interface

	// storageClass は、PVC に指定する StorageClass。空なら指定しない。
	storageClass string

	// accessMode は、PVC のアクセスモード。
	accessMode corev1.PersistentVolumeAccessMode

	// expandPollInterval は、拡張が反映されたかを確かめる間隔(テストで短くできる)。
	expandPollInterval time.Duration

	// expandTimeout は、拡張が反映されるのを待つ最長の時間(テストで短くできる)。
	expandTimeout time.Duration
}

// NewKubernetesVolumeOperator は、KubernetesVolumeOperator を作る。
//
// 引数:
//   - clientset: k8s のクライアント。
//   - settings: StorageClass とアクセスモード。アクセスモードが空なら ReadWriteOnce。
//
// 戻り値:
//   - 作成した KubernetesVolumeOperator。
func NewKubernetesVolumeOperator(clientset kubernetes.Interface, settings VolumeSettings) *KubernetesVolumeOperator {
	// アクセスモードの既定は、ReadWriteOnce。
	accessMode := corev1.ReadWriteOnce
	if settings.AccessMode != "" {
		accessMode = corev1.PersistentVolumeAccessMode(settings.AccessMode)
	}
	// 値を持たせて返す。
	return &KubernetesVolumeOperator{
		clientset: clientset, storageClass: settings.StorageClass, accessMode: accessMode,
		expandPollInterval: defaultExpandPollInterval, expandTimeout: defaultExpandTimeout,
	}
}

// storageQuantity は、MB(実際は MiB)の大きさを、k8s の量にする。
//
// 引数:
//   - sizeMB: 大きさ(MB)。
//
// 戻り値:
//   - k8s の量(MiB 単位)。
func storageQuantity(sizeMB int) resource.Quantity {
	// MiB の単位で、バイト数にする。
	return *resource.NewQuantity(int64(sizeMB)*1024*1024, resource.BinarySI)
}

// checkVolumeTarget は、触ってよい namespace と名前かを確かめる(k8s に触れる前の安全装置)。
//
// 引数:
//   - namespaceName: namespace の名前(プロジェクトの ID の形)。
//   - claimName: PVC の名前(volume の ID の形)。
//
// 戻り値:
//   - 不正なら、名前の形のエラー。
func checkVolumeTarget(namespaceName string, claimName string) error {
	// プロジェクトのものではない namespace には、触れない。
	if validateError := validateProjectNamespace(namespaceName); validateError != nil {
		return validateError
	}
	// volume のものではない名前の PVC には、触れない。
	if !volumeNamePattern.MatchString(claimName) {
		return fmt.Errorf("%w: %q", activity.ErrInvalidVolumeName, claimName)
	}
	return nil
}

// EnsureVolumeClaim は、PVC が無ければ作る。すでにあれば何もしない(冪等。大きさは変えない)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - definition: 作る PVC の内容。
//
// 戻り値:
//   - 名前の形が不正なとき、k8s への依頼に失敗したときのエラー。
func (operator *KubernetesVolumeOperator) EnsureVolumeClaim(requestContext context.Context, definition activity.VolumeClaimDefinition) error {
	// 触ってよい namespace と名前かを確かめる。
	if checkError := checkVolumeTarget(definition.Namespace, definition.Name); checkError != nil {
		return checkError
	}
	// PVC の内容を組み立てる。StorageClass は、指定があるときだけ付ける。
	claim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: definition.Name, Namespace: definition.Namespace, Labels: map[string]string{projectIDLabel: definition.Namespace}},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{operator.accessMode},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: storageQuantity(definition.SizeMB)},
			},
		},
	}
	if operator.storageClass != "" {
		storageClass := operator.storageClass
		claim.Spec.StorageClassName = &storageClass
	}
	// 作る。すでにあれば、目的どおりなので成功にする。
	_, createError := operator.clientset.CoreV1().PersistentVolumeClaims(definition.Namespace).Create(requestContext, claim, metav1.CreateOptions{})
	if createError != nil && !apierrors.IsAlreadyExists(createError) {
		return fmt.Errorf("PVC %s を作れませんでした: %w", definition.Name, createError)
	}
	return nil
}

// ExpandVolumeClaim は、PVC があれば、大きさを広げ、容量が増えるのを待つ。無ければ、何もせず成功する。
//
// 現在の要求が、すでにその大きさ以上なら、広げない(縮めない)。広げたあと(または、すでに広げてあるとき)は、
// PVC の実際の容量(status.capacity)が、その大きさになるまで、間隔をあけて確かめる。待つ時間を超えたら ErrVolumeExpandTimeout。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - definition: 拡張する PVC の内容(SizeMB が、新しい大きさ)。
//   - heartbeat: 待っている間に、生きていることを伝える関数。
//
// 戻り値:
//   - 名前の形が不正なとき、k8s への依頼に失敗したとき、時間切れのときのエラー。
func (operator *KubernetesVolumeOperator) ExpandVolumeClaim(requestContext context.Context, definition activity.VolumeClaimDefinition, heartbeat func()) error {
	// 触ってよい namespace と名前かを確かめる。
	if checkError := checkVolumeTarget(definition.Namespace, definition.Name); checkError != nil {
		return checkError
	}
	claims := operator.clientset.CoreV1().PersistentVolumeClaims(definition.Namespace)
	// PVC を読む。無ければ、まだマウントされていないので、何もせず成功にする。
	claim, getError := claims.Get(requestContext, definition.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(getError) {
		return nil
	}
	if getError != nil {
		return fmt.Errorf("PVC %s を取得できませんでした: %w", definition.Name, getError)
	}
	// 要求が、新しい大きさより小さいときだけ、広げる。
	wanted := storageQuantity(definition.SizeMB)
	if requested := claim.Spec.Resources.Requests[corev1.ResourceStorage]; requested.Cmp(wanted) < 0 {
		claim.Spec.Resources.Requests[corev1.ResourceStorage] = wanted
		if _, updateError := claims.Update(requestContext, claim, metav1.UpdateOptions{}); updateError != nil {
			return fmt.Errorf("PVC %s を拡張できませんでした: %w", definition.Name, updateError)
		}
	}
	// 実際の容量が、新しい大きさになるまで待つ。
	deadline := time.Now().Add(operator.expandTimeout)
	for {
		// 生きていることを伝える。
		heartbeat()
		// 今の容量を読む。
		current, currentError := claims.Get(requestContext, definition.Name, metav1.GetOptions{})
		if currentError != nil {
			return fmt.Errorf("PVC %s を取得できませんでした: %w", definition.Name, currentError)
		}
		// 容量が、新しい大きさ以上になっていれば、終わり。
		if capacity := current.Status.Capacity[corev1.ResourceStorage]; capacity.Cmp(wanted) >= 0 {
			return nil
		}
		// 待つ時間を超えたら、時間切れ。
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: %s", activity.ErrVolumeExpandTimeout, definition.Name)
		}
		// 次の確認まで待つ(キャンセルされたら、すぐ終わる)。
		select {
		case <-requestContext.Done():
			return requestContext.Err()
		case <-time.After(operator.expandPollInterval):
		}
	}
}

// DeleteVolumeClaim は、PVC を削除する(無くてもエラーにしない。冪等)。
//
// 引数:
//   - requestContext: キャンセルや期限を伝える context。
//   - namespaceName: namespace の名前(プロジェクトの ID の形)。
//   - claimName: PVC の名前(volume の ID の形)。
//
// 戻り値:
//   - 名前の形が不正なとき、k8s への依頼に失敗したときのエラー。
func (operator *KubernetesVolumeOperator) DeleteVolumeClaim(requestContext context.Context, namespaceName string, claimName string) error {
	// 触ってよい namespace と名前かを確かめる。
	if checkError := checkVolumeTarget(namespaceName, claimName); checkError != nil {
		return checkError
	}
	// 削除する。無ければ、すでに目的どおりなので、成功にする。
	deleteError := operator.clientset.CoreV1().PersistentVolumeClaims(namespaceName).Delete(requestContext, claimName, metav1.DeleteOptions{})
	if deleteError != nil && !apierrors.IsNotFound(deleteError) {
		return fmt.Errorf("PVC %s を削除できませんでした: %w", claimName, deleteError)
	}
	return nil
}
