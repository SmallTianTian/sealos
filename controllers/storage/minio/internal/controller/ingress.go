/*
Copyright 2022 labring.

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

package controller

import (
	"context"
	"fmt"
	"time"

	apisix "github.com/apache/apisix-ingress-controller/pkg/kube/apisix/apis/config/v2beta3"
	miniov1 "github.com/labring/sealos/controllers/storage/minio/api/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	AuthType = "basicAuth"
)

func (r *MinioReconciler) syncApisixIngress(ctx context.Context, minio *miniov1.Minio, s3Host, consoleHost string) error {
	// 1. sync ApisixRoute
	apisixRoute := &apisix.ApisixRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      minio.Name,
			Namespace: minio.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, apisixRoute, func() error {
		expectRoute := r.createApisixRoute(minio, s3Host, consoleHost)
		switch len(apisixRoute.Spec.HTTP) {
		case 2:
			apisixRoute.Spec.HTTP[1].Name = expectRoute.Spec.HTTP[1].Name
			apisixRoute.Spec.HTTP[1].Match = expectRoute.Spec.HTTP[1].Match
			apisixRoute.Spec.HTTP[1].Backends = expectRoute.Spec.HTTP[1].Backends
			apisixRoute.Spec.HTTP[1].Timeout = expectRoute.Spec.HTTP[1].Timeout
			apisixRoute.Spec.HTTP[1].Authentication = expectRoute.Spec.HTTP[1].Authentication
			fallthrough
		case 1:
			apisixRoute.Spec.HTTP[0].Name = expectRoute.Spec.HTTP[0].Name
			apisixRoute.Spec.HTTP[0].Match = expectRoute.Spec.HTTP[0].Match
			apisixRoute.Spec.HTTP[0].Backends = expectRoute.Spec.HTTP[0].Backends
			apisixRoute.Spec.HTTP[0].Timeout = expectRoute.Spec.HTTP[0].Timeout
			apisixRoute.Spec.HTTP[0].Authentication = expectRoute.Spec.HTTP[0].Authentication
		case 0:
			apisixRoute.Spec.HTTP = expectRoute.Spec.HTTP
		default:
			return fmt.Errorf("exceeding expected quantity")
		}
		return controllerutil.SetControllerReference(minio, apisixRoute, r.Scheme)
	}); err != nil {
		return err
	}

	// 2. sync ApisixTls
	apisixTLS := &apisix.ApisixTls{
		ObjectMeta: metav1.ObjectMeta{
			Name:      minio.Name,
			Namespace: minio.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, apisixTLS, func() error {
		expectTLS := r.createApisixTLS(minio, s3Host, consoleHost)
		if apisixTLS.Spec != nil {
			apisixTLS.Spec.Hosts = expectTLS.Spec.Hosts
			apisixTLS.Spec.Secret = expectTLS.Spec.Secret
		} else {
			apisixTLS.Spec = expectTLS.Spec
		}
		return controllerutil.SetControllerReference(minio, apisixTLS, r.Scheme)
	}); err != nil {
		return err
	}
	return nil
}

func (r *MinioReconciler) syncNginxIngress(ctx context.Context, minio *miniov1.Minio, s3Host, consoleHost string) error {
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      minio.Name,
			Namespace: minio.Namespace,
		},
	}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		expectIngress := r.createNginxIngress(minio, s3Host, consoleHost)
		ingress.ObjectMeta.Labels = expectIngress.ObjectMeta.Labels
		ingress.ObjectMeta.Annotations = expectIngress.ObjectMeta.Annotations
		ingress.Spec.Rules = expectIngress.Spec.Rules
		ingress.Spec.TLS = expectIngress.Spec.TLS
		return controllerutil.SetControllerReference(minio, ingress, r.Scheme)
	}); err != nil {
		return err
	}
	return nil
}

func (r *MinioReconciler) createNginxIngress(minio *miniov1.Minio, s3Host, consoleHost string) *networkingv1.Ingress {
	cors := fmt.Sprintf("https://%s,https://*.%s", r.minioDomain, r.minioDomain)

	objectMeta := metav1.ObjectMeta{
		Name:      minio.Name,
		Namespace: minio.Namespace,
		Annotations: map[string]string{
			"kubernetes.io/ingress.class":                        "nginx",
			"nginx.ingress.kubernetes.io/rewrite-target":         "/",
			"nginx.ingress.kubernetes.io/proxy-send-timeout":     "86400",
			"nginx.ingress.kubernetes.io/proxy-read-timeout":     "86400",
			"nginx.ingress.kubernetes.io/enable-cors":            "true",
			"nginx.ingress.kubernetes.io/cors-allow-origin":      cors,
			"nginx.ingress.kubernetes.io/cors-allow-methods":     "GET, HEAD, PUT, POST, DELETE",
			"nginx.ingress.kubernetes.io/cors-allow-credentials": "false",
		},
	}

	pathType := networkingv1.PathTypePrefix

	var rules []networkingv1.IngressRule
	tls := networkingv1.IngressTLS{
		Hosts:      []string{},
		SecretName: r.secretName,
	}

	if s3Host != "" {
		rule := networkingv1.IngressRule{
			Host: s3Host,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{
						PathType: &pathType,
						Path:     "/",
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: fmt.Sprintf("s3-%s", minio.Name),
								Port: networkingv1.ServiceBackendPort{
									Number: int32(s3Port),
								},
							},
						},
					}},
				},
			},
		}
		rules = append(rules, rule)
		tls.Hosts = append(tls.Hosts, s3Host)
	}

	if consoleHost != "" {
		rule := networkingv1.IngressRule{
			Host: consoleHost,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{
						PathType: &pathType,
						Path:     "/",
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: fmt.Sprintf("console-%s", minio.Name),
								Port: networkingv1.ServiceBackendPort{
									Number: int32(consolePort),
								},
							},
						},
					}},
				},
			},
		}
		rules = append(rules, rule)
		tls.Hosts = append(tls.Hosts, consoleHost)
	}

	ingress := &networkingv1.Ingress{
		ObjectMeta: objectMeta,
		Spec: networkingv1.IngressSpec{
			Rules: rules,
			TLS:   []networkingv1.IngressTLS{tls},
		},
	}
	return ingress
}

func (r *MinioReconciler) createApisixRoute(minio *miniov1.Minio, s3Host, consoleHost string) *apisix.ApisixRoute {
	// config proxy_read_timeout and proxy_send_timeout
	upstreamTimeout := &apisix.UpstreamTimeout{
		Read: metav1.Duration{
			Duration: time.Hour,
		},
		Send: metav1.Duration{
			Duration: time.Hour,
		},
	}

	// ApisixRoute
	apisixRoute := &apisix.ApisixRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      minio.Name,
			Namespace: minio.Namespace,
		},
		Spec: apisix.ApisixRouteSpec{
			HTTP: []apisix.ApisixRouteHTTP{},
		},
	}

	if s3Host != "" {
		apisixRoute.Spec.HTTP = append(apisixRoute.Spec.HTTP, apisix.ApisixRouteHTTP{
			Name: fmt.Sprintf("s3-%s", minio.Name),
			Match: apisix.ApisixRouteHTTPMatch{
				Hosts: []string{s3Host},
				Paths: []string{"/*"},
			},
			Backends: []apisix.ApisixRouteHTTPBackend{
				{
					ServiceName: minio.Name,
					ServicePort: intstr.FromInt(s3Port),
				},
			},
			Timeout: upstreamTimeout,
			Authentication: apisix.ApisixRouteAuthentication{
				Enable: false,
				Type:   AuthType,
			},
		})
	}
	if consoleHost != "" {
		apisixRoute.Spec.HTTP = append(apisixRoute.Spec.HTTP, apisix.ApisixRouteHTTP{
			Name: fmt.Sprintf("console-%s", minio.Name),
			Match: apisix.ApisixRouteHTTPMatch{
				Hosts: []string{consoleHost},
				Paths: []string{"/*"},
			},
			Backends: []apisix.ApisixRouteHTTPBackend{
				{
					ServiceName: minio.Name,
					ServicePort: intstr.FromInt(consolePort),
				},
			},
			Timeout: upstreamTimeout,
			Authentication: apisix.ApisixRouteAuthentication{
				Enable: false,
				Type:   AuthType,
			},
		})
	}
	return apisixRoute
}

func (r *MinioReconciler) createApisixTLS(minio *miniov1.Minio, s3Host, consoleHost string) *apisix.ApisixTls {
	apisixTLS := &apisix.ApisixTls{
		ObjectMeta: metav1.ObjectMeta{
			Name:      minio.Name,
			Namespace: minio.Namespace,
		},
		Spec: &apisix.ApisixTlsSpec{
			Hosts: []apisix.HostType{},
			Secret: apisix.ApisixSecret{
				Name:      r.secretName,
				Namespace: r.secretNamespace,
			},
		},
	}

	if s3Host != "" {
		apisixTLS.Spec.Hosts = append(apisixTLS.Spec.Hosts, apisix.HostType(s3Host))
	}
	if consoleHost != "" {
		apisixTLS.Spec.Hosts = append(apisixTLS.Spec.Hosts, apisix.HostType(consoleHost))
	}
	return apisixTLS
}
