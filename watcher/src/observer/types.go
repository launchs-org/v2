package observer

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// podObject は、informer から受け取る Pod の型。
type podObject = *corev1.Pod

// deploymentObject は、informer から受け取る Deployment の型。
type deploymentObject = *appsv1.Deployment

// metav1ListOptions は、k8s の一覧の取得の条件の型(ラベルで絞り込むために使う)。
type metav1ListOptions = metav1.ListOptions
