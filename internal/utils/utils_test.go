/*
Copyright 2022.

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

package utils

import (
	"context"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	//+kubebuilder:scaffold:imports
)

func TestDevicefinder(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Utilities Suite")
}

var _ = Describe("GetCurrentClusterVersion", func() {
	var (
		clusterVersion *configv1.ClusterVersion
	)

	Context("when there are completed versions in the history", func() {
		BeforeEach(func() {
			clusterVersion = &configv1.ClusterVersion{
				Status: configv1.ClusterVersionStatus{
					History: []configv1.UpdateHistory{
						{State: "Completed", Version: "4.6.1"},
					},
					Desired: configv1.Release{
						Version: "4.7.0",
					},
				},
			}
		})

		It("should return the completed version", func() {
			version, err := GetCurrentClusterVersion(clusterVersion)
			Expect(err).ToNot(HaveOccurred())
			Expect(version.String()).To(Equal("4.6.1"))
		})
	})

	Context("when there are no completed versions in the history", func() {
		BeforeEach(func() {
			clusterVersion = &configv1.ClusterVersion{
				Status: configv1.ClusterVersionStatus{
					History: []configv1.UpdateHistory{
						{State: "Partial", Version: "4.6.1"},
					},
					Desired: configv1.Release{
						Version: "4.7.0",
					},
				},
			}
		})

		It("should return the desired version", func() {
			version, err := GetCurrentClusterVersion(clusterVersion)
			Expect(err).ToNot(HaveOccurred())
			Expect(version.String()).To(Equal("4.7.0"))
		})
	})
})

var _ = Describe("ParseAndReturnVersion", func() {
	Context("when the version string is valid", func() {
		It("should return the parsed version", func() {
			versionStr := "4.6.1"
			version, err := parseAndReturnVersion(versionStr)
			Expect(err).ToNot(HaveOccurred())
			Expect(version.String()).To(Equal(versionStr))
		})
	})

	Context("when the version string is invalid", func() {
		It("should return an error", func() {
			versionStr := "invalid-version"
			version, err := parseAndReturnVersion(versionStr)
			Expect(err).To(HaveOccurred())
			Expect(version).To(BeNil())
		})
	})
})

func TestIsOpenShiftSupported(t *testing.T) {
	tests := []struct {
		ibmVersion string
		ocpVersion string
		expected   bool
	}{
		{"5.1.5.0", "4.9.0", true},        // Expected to be supported
		{"5.1.5.0", "4.12.1", false},      // Not in the supported list
		{"5.1.7.0", "4.11.43", true},      // Supported
		{"5.1.7.0", "4.13.34", false},     // Not supported
		{"5.1.9.1", "4.12.7", true},       // Supported
		{"5.1.9.1", "4.15.0", false},      // Not supported
		{"5.2.2.0", "4.17.3", true},       // Supported
		{"5.2.2.0", "4.18.1", false},      // Not supported
		{"5.2.2.0", "4.15.17", true},      // Supported
		{"invalid_version", "4.9", false}, // Invalid IBM Fusion Access version
	}

	for _, tt := range tests {
		result := IsOpenShiftSupported(tt.ibmVersion, *semver.MustParse(tt.ocpVersion))
		if result != tt.expected {
			t.Errorf("IsOpenShiftSupported(%s, %s) = %v; expected %v", tt.ibmVersion, tt.ocpVersion, result, tt.expected)
		}
	}
}

var _ = Describe("Image Pull Checker", func() {
	var (
		client      *fake.Clientset
		namespace   string
		image       string
		testTimeout time.Duration
	)

	BeforeEach(func() {
		client = fake.NewSimpleClientset()
		namespace = "default"
		image = "test.registry.io/valid/image:latest"
		testTimeout = 3 * time.Second
	})

	Describe("PollPodPullStatus", func() {
		It("should detect ErrImagePull and return error", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "image-fail-pod",
					Namespace: namespace,
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Reason:  "ErrImagePull",
									Message: "image not found",
								},
							},
						},
					},
				},
			}

			_, err := client.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()

			success, err := PollPodPullStatus(ctx, client, namespace, pod.Name)
			Expect(success).To(BeFalse())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("image pull failed"))
		})

		It("should detect success if pod is Running", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "image-success-pod",
					Namespace: namespace,
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{
									StartedAt: metav1.Now(),
								},
							},
						},
					},
				},
			}

			_, err := client.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()

			success, err := PollPodPullStatus(ctx, client, namespace, pod.Name)
			Expect(success).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})

		It("should timeout if pod never updates", func() {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "image-hang-pod",
					Namespace: namespace,
				},
				Status: corev1.PodStatus{}, // no ContainerStatuses
			}

			_, err := client.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()

			success, err := PollPodPullStatus(ctx, client, namespace, pod.Name)
			Expect(success).To(BeFalse())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("timeout"))
		})
	})

	Describe("CreateImageCheckPod", func() {
		It("should create a pod successfully", func() {
			ctx := context.Background()
			podName, err := CreateImageCheckPod(ctx, client, namespace, image)
			Expect(err).NotTo(HaveOccurred())
			Expect(podName).To(Equal(CheckPodName))

			pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Spec.Containers[0].Image).To(Equal(image))
		})
	})
})
