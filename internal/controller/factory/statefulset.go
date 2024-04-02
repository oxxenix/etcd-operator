/*
Copyright 2024 The etcd-operator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package factory

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	etcdaenixiov1alpha1 "github.com/aenix-io/etcd-operator/api/v1alpha1"
)

func CreateOrUpdateStatefulSet(
	ctx context.Context,
	cluster *etcdaenixiov1alpha1.EtcdCluster,
	rclient client.Client,
	rscheme *runtime.Scheme,
) error {
	podMetadata := metav1.ObjectMeta{
		Labels: NewLabelsBuilder().WithName().WithInstance(cluster.Name).WithManagedBy(),
	}

	if cluster.Spec.PodSpec.PodMetadata != nil {
		if cluster.Spec.PodSpec.PodMetadata.Name != "" {
			podMetadata.GenerateName = cluster.Spec.PodSpec.PodMetadata.Name
		}

		for key, value := range cluster.Spec.PodSpec.PodMetadata.Labels {
			podMetadata.Labels[key] = value
		}

		podMetadata.Annotations = cluster.Spec.PodSpec.PodMetadata.Annotations
	}

	podEnv := []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	}
	podEnv = append(podEnv, cluster.Spec.PodSpec.ExtraEnv...)

	volumeClaimTemplates := []corev1.PersistentVolumeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:        GetPVCName(cluster),
				Labels:      cluster.Spec.Storage.VolumeClaimTemplate.Labels,
				Annotations: cluster.Spec.Storage.VolumeClaimTemplate.Annotations,
			},
			Spec:   cluster.Spec.Storage.VolumeClaimTemplate.Spec,
			Status: cluster.Spec.Storage.VolumeClaimTemplate.Status,
		},
	}

	volumes, volumeMounts := generateVolumeConfig(cluster)

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: cluster.Namespace,
			Name:      cluster.Name,
		},
		Spec: appsv1.StatefulSetSpec{
			// initialize static fields that cannot be changed across updates.
			Replicas:            cluster.Spec.Replicas,
			ServiceName:         cluster.Name,
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Selector: &metav1.LabelSelector{
				MatchLabels: NewLabelsBuilder().WithName().WithInstance(cluster.Name).WithManagedBy(),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: podMetadata,
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "etcd",
							Image:           cluster.Spec.PodSpec.Image,
							ImagePullPolicy: cluster.Spec.PodSpec.ImagePullPolicy,
							Command:         generateEtcdCommand(cluster),
							Ports: []corev1.ContainerPort{
								{Name: "peer", ContainerPort: 2380},
								{Name: "client", ContainerPort: 2379},
							},
							Resources: cluster.Spec.PodSpec.Resources,
							EnvFrom: []corev1.EnvFromSource{
								{
									ConfigMapRef: &corev1.ConfigMapEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: GetClusterStateConfigMapName(cluster),
										},
									},
								},
							},
							Env:          podEnv,
							VolumeMounts: volumeMounts,
							// StartupProbe: &corev1.Probe{
							// 	ProbeHandler: corev1.ProbeHandler{
							// 		HTTPGet: &corev1.HTTPGetAction{

							// 			Scheme: "HTTPS",
							// 			Path:   "/readyz?serializable=false",
							// 			Port:   intstr.FromInt32(2379),
							// 		},
							// 	},
							// 	InitialDelaySeconds: 1,
							// 	PeriodSeconds:       5,
							// },
							// LivenessProbe: &corev1.Probe{
							// 	ProbeHandler: corev1.ProbeHandler{
							// 		HTTPGet: &corev1.HTTPGetAction{
							// 			Scheme: "HTTPS",
							// 			Path:   "/livez",
							// 			Port:   intstr.FromInt32(2379),
							// 		},
							// 	},
							// 	InitialDelaySeconds: 5,
							// 	PeriodSeconds:       5,
							// },
							// ReadinessProbe: &corev1.Probe{
							// 	ProbeHandler: corev1.ProbeHandler{
							// 		HTTPGet: &corev1.HTTPGetAction{
							// 			Scheme: "HTTPS",
							// 			Path:   "/readyz",
							// 			Port:   intstr.FromInt32(2379),
							// 		},
							// 	},
							// 	InitialDelaySeconds: 5,
							// 	PeriodSeconds:       5,
							// },
						},
					},
					ImagePullSecrets:              cluster.Spec.PodSpec.ImagePullSecrets,
					Affinity:                      cluster.Spec.PodSpec.Affinity,
					NodeSelector:                  cluster.Spec.PodSpec.NodeSelector,
					TopologySpreadConstraints:     cluster.Spec.PodSpec.TopologySpreadConstraints,
					Tolerations:                   cluster.Spec.PodSpec.Tolerations,
					SecurityContext:               cluster.Spec.PodSpec.SecurityContext,
					PriorityClassName:             cluster.Spec.PodSpec.PriorityClassName,
					TerminationGracePeriodSeconds: cluster.Spec.PodSpec.TerminationGracePeriodSeconds,
					SchedulerName:                 cluster.Spec.PodSpec.SchedulerName,
					RuntimeClassName:              cluster.Spec.PodSpec.RuntimeClassName,
					Volumes:                       volumes,
				},
			},
			VolumeClaimTemplates: volumeClaimTemplates,
		},
	}

	if err := ctrl.SetControllerReference(cluster, statefulSet, rscheme); err != nil {
		return fmt.Errorf("cannot set controller reference: %w", err)
	}

	return reconcileStatefulSet(ctx, rclient, cluster.Name, statefulSet)
}

func generateVolumeConfig(cluster *etcdaenixiov1alpha1.EtcdCluster) ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := []corev1.Volume{}
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "data",
			ReadOnly:  false,
			MountPath: "/var/run/etcd",
		},
	}

	if cluster.Spec.Storage.EmptyDir != nil {
		volumes = append(volumes, []corev1.Volume{
			{
				Name:         "data",
				VolumeSource: corev1.VolumeSource{EmptyDir: cluster.Spec.Storage.EmptyDir},
			},
		}...)
	} else {
		volumes = append(volumes, []corev1.Volume{
			{
				Name: "data",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: GetPVCName(cluster),
					},
				},
			},
		}...)
	}

	if cluster.Spec.Security.Peer != nil {
		volumes = append(volumes,
			[]corev1.Volume{
				{
					Name: "ca-peer-cert",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: cluster.Spec.Security.Peer.CaSecretName,
						},
					},
				},
				{
					Name: "peer-cert",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: cluster.Spec.Security.Peer.TlsSecretName,
						},
					},
				},
			}...)

		volumeMounts = append(volumeMounts, []corev1.VolumeMount{
			{
				Name:      "ca-peer-cert",
				ReadOnly:  true,
				MountPath: "/etc/etcd/pki/peer/ca",
			},
			{
				Name:      "peer-cert",
				ReadOnly:  true,
				MountPath: "/etc/etcd/pki/peer/cert",
			},
		}...)
	}

	if cluster.Spec.Security.ClientServer != nil {
		volumes = append(volumes,
			[]corev1.Volume{
				{
					Name: "ca-server-cert",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: cluster.Spec.Security.ClientServer.CaSecretName,
						},
					},
				},
				{
					Name: "server-cert",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: cluster.Spec.Security.ClientServer.TlsSecretName,
						},
					},
				},
			}...)

		volumeMounts = append(volumeMounts, []corev1.VolumeMount{
			{
				Name:      "ca-server-cert",
				ReadOnly:  true,
				MountPath: "/etc/etcd/pki/server/ca",
			},
			{
				Name:      "server-cert",
				ReadOnly:  true,
				MountPath: "/etc/etcd/pki/server/cert",
			},
		}...)
	}

	return volumes, volumeMounts
}

func generateEtcdCommand(cluster *etcdaenixiov1alpha1.EtcdCluster) []string {

	peerTlsSettings := []string{"--peer-auto-tls"}

	if cluster.Spec.Security.Peer.TlsSecretName != "" {
		peerTlsSettings = []string{
			"--peer-trusted-ca-file=/etc/etcd/pki/peer/ca/ca.crt",
			"--peer-cert-file=/etc/etcd/pki/peer/cert/tls.crt",
			"--peer-key-file=/etc/etcd/pki/peer/cert/tls.key",
			// "--peer-client-cert-auth",
		}
	}

	clientTlsSettings := []string{}
	serverProtocol := "http"

	if cluster.Spec.Security.Peer.TlsSecretName != "" {
		clientTlsSettings = []string{
			// "--auto-tls",
			"--trusted-ca-file=/etc/etcd/pki/server/ca/ca.crt",
			"--cert-file=/etc/etcd/pki/server/cert/tls.crt",
			"--key-file=/etc/etcd/pki/server/cert/tls.key",
			// "--peer-client-cert-auth",

			// "--client-cert-auth",
		}

		serverProtocol = "https"
	}

	command := []string{
		"etcd",
		"--name=$(POD_NAME)",
		"--listen-peer-urls=https://0.0.0.0:2380",
		// for first version disable TLS for client access
		fmt.Sprintf("--listen-client-urls=%s://0.0.0.0:2379", serverProtocol),
		fmt.Sprintf("--initial-advertise-peer-urls=https://$(POD_NAME).%s.$(POD_NAMESPACE).svc:2380", cluster.Name),
		// "--data-dir=/var/run/etcd/default.etcd",
		//
		fmt.Sprintf("--advertise-client-urls=%s://etcd-sample-client.$(POD_NAMESPACE).svc:2379", serverProtocol),
	}

	command = append(command, peerTlsSettings...)
	command = append(command, clientTlsSettings...)

	for name, value := range cluster.Spec.PodSpec.ExtraArgs {
		command = append(command, fmt.Sprintf("--%s=%s", name, value))
	}

	return command
}
