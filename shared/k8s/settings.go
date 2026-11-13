// Package k8s は、k8s のクライアントを作る共通の部品を持つ。
//
// k8s につなぐコンテナ(今はワーカー。今後は、ログ収集など)が、同じ環境変数、
// 同じ接続の作り方を使えるように、shared に置く。
package k8s

import (
	"errors"
	"fmt"

	sharedconfig "launchs/shared/config"
)

// Access は、k8s につながる方法。
type Access string

const (
	// AccessNone は、k8s につながない(開発用)。k8s を操作する処理は、行わずにログに出すだけにする。
	AccessNone Access = "none"

	// AccessKubeconfig は、kubeconfig のファイルで接続する(開発用)。
	AccessKubeconfig Access = "kubeconfig"

	// AccessInCluster は、Pod の ServiceAccount で接続する(本番)。
	AccessInCluster Access = "in-cluster"
)

// Settings は、k8s への接続の設定。
type Settings struct {
	// Access は、k8s につながる方法(環境変数 KUBERNETES_ACCESS)。
	Access Access

	// KubeconfigPath は、Access が kubeconfig のときに読む kubeconfig のパス(環境変数 KUBECONFIG)。
	KubeconfigPath string
}

// ReadSettings は、環境変数から、k8s への接続の設定を読む。
//
// 読む環境変数:
//   - KUBERNETES_ACCESS: 接続の方法(none、kubeconfig、in-cluster)。必須。
//     none は何も操作せずログに出すだけなので、本番で誤って使わないよう、省略はできない。
//   - KUBECONFIG: kubeconfig のパス。KUBERNETES_ACCESS が kubeconfig のとき必須。
//
// 値の検査はここでは行わない(読み込みの問題は reader に溜まる)。値の検査は Validate で行う。
//
// 引数:
//   - reader: 環境変数を読む EnvironmentReader。
//
// 戻り値:
//   - 読み込んだ設定。
func ReadSettings(reader *sharedconfig.EnvironmentReader) Settings {
	// 接続の方法(必須)と、kubeconfig のパス(省略可)を読む。
	return Settings{
		Access:         Access(reader.RequiredString("KUBERNETES_ACCESS")),
		KubeconfigPath: reader.OptionalString("KUBECONFIG", ""),
	}
}

// Validate は、設定の値が正しいかを確かめる。
//
// 接続の方法が、決まった3つのどれかであること、kubeconfig のときはパスがあることを確かめる。
// 接続の方法が空のときは、ReadSettings が(必須の環境変数が無いと)すでに報告するので、重ねて報告しない。
//
// 戻り値:
//   - 値が正しくないときのエラー。正しければ nil。
func (settings Settings) Validate() error {
	// 接続の方法ごとに、必要な設定がそろっているかを確かめる。
	switch settings.Access {
	case AccessNone, AccessInCluster:
		// 追加の設定は要らない。
		return nil
	case AccessKubeconfig:
		// kubeconfig で接続するときは、そのパスが要る。
		if settings.KubeconfigPath == "" {
			return errors.New("環境変数 KUBERNETES_ACCESS が kubeconfig のときは、環境変数 KUBECONFIG が必要です")
		}
		return nil
	case "":
		// 空は、必須の環境変数が無いという問題として、すでに報告されている。
		return nil
	default:
		// 決まった値のどれでもないときは、エラーにする。
		return fmt.Errorf("環境変数 KUBERNETES_ACCESS は none、kubeconfig、in-cluster のどれかでなければなりません: %q", settings.Access)
	}
}
