package metrics_test

import (
	"log"

	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	. "github.com/onsi/gomega"    //nolint:revive,staticcheck // dot import is idiomatic for Gomega

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/monitoring"
)

// mtlsComponents defines the component matrix for mTLS testing.
// Each entry maps a component to its Service(s) and ServiceMonitor(s).
var mtlsComponents = []struct {
	component      string
	services       []monitoring.MTLSComponentConfig
	serviceMonitor string
}{
	{
		component: "TektonPipeline",
		services: []monitoring.MTLSComponentConfig{
			{ServiceName: "tekton-pipelines-controller", HTTPPortName: "http-metrics", HTTPSPortName: "https-metrics"},
			{ServiceName: "tekton-events-controller", HTTPPortName: "http-metrics", HTTPSPortName: "https-metrics"},
			{ServiceName: "tekton-pipelines-remote-resolvers", HTTPPortName: "http-metrics", HTTPSPortName: "https-metrics"},
		},
		serviceMonitor: "openshift-pipelines-monitor",
	},
	{
		component: "TektonTrigger",
		services: []monitoring.MTLSComponentConfig{
			{ServiceName: "tekton-triggers-controller", HTTPPortName: "http-metrics", HTTPSPortName: "https-metrics"},
		},
		serviceMonitor: "openshift-triggers-monitor",
	},
	{
		component: "TektonChain",
		services: []monitoring.MTLSComponentConfig{
			{ServiceName: "tekton-chains-metrics", HTTPPortName: "http-metrics", HTTPSPortName: "https-metrics"},
		},
		serviceMonitor: "openshift-chains-monitor",
	},
	{
		component: "TektonPruner",
		services: []monitoring.MTLSComponentConfig{
			{ServiceName: "tekton-pruner-controller", HTTPPortName: "http-metrics", HTTPSPortName: "https-metrics"},
		},
		serviceMonitor: "openshift-pruner-monitor",
	},
	{
		component: "OpenShiftPipelinesAsCode",
		services: []monitoring.MTLSComponentConfig{
			{ServiceName: "pipelines-as-code-controller", HTTPPortName: "http-metrics", HTTPSPortName: "https-metrics"},
			{ServiceName: "pipelines-as-code-watcher", HTTPPortName: "http-metrics", HTTPSPortName: "https-metrics"},
		},
		serviceMonitor: "pipelines-as-code-controller-monitor",
	},
	{
		component: "TektonResult",
		services: []monitoring.MTLSComponentConfig{
			{ServiceName: "tekton-results-watcher", HTTPPortName: "http-metrics", HTTPSPortName: "https-metrics"},
		},
		serviceMonitor: "openshift-results-watcher-monitor",
	},
}

// pacExtraServiceMonitor is the additional ServiceMonitor for PAC.
const pacExtraServiceMonitor = "pipelines-as-code-monitor"

var _ = Describe("Prometheus metrics mTLS (enableMetricsMTLS)", Serial, Ordered,
	Label("metrics", "e2e", "admin"), func() {

		var originalMTLSValue *bool

		BeforeEach(func() {
			lastNamespace = config.TargetNamespace
		})

		BeforeAll(func() {
			// Save the original enableMetricsMTLS value
			originalMTLSValue = monitoring.GetEnableMetricsMTLS()
			log.Printf("Original enableMetricsMTLS value: %v", originalMTLSValue)
		})

		AfterAll(func() {
			// Restore the original enableMetricsMTLS value
			if originalMTLSValue != nil {
				Expect(monitoring.SetEnableMetricsMTLS(*originalMTLSValue)).To(Succeed())
			} else {
				// If it was unset, disable it to restore the default state
				Expect(monitoring.SetEnableMetricsMTLS(false)).To(Succeed())
			}
			log.Printf("Restored enableMetricsMTLS to original value")

			err := monitoring.WaitForTektonConfigReady(sharedClients)
			Expect(err).NotTo(HaveOccurred(), "TektonConfig did not reach Ready after restoring enableMetricsMTLS")
		})

		// ── mTLS enabled assertions ─────────────────────────────────────────

		Describe("mTLS enabled", func() {
			BeforeAll(func() {
				Expect(monitoring.SetEnableMetricsMTLS(true)).To(Succeed())
				err := monitoring.WaitForTektonConfigReady(sharedClients)
				Expect(err).NotTo(HaveOccurred(), "TektonConfig did not reach Ready after enabling mTLS")
			})

			for _, comp := range mtlsComponents {
				Context(comp.component, func() {
					for _, svc := range comp.services {
						svc.Namespace = config.TargetNamespace
						svc.ComponentName = comp.component

						It("Service "+svc.ServiceName+" has mTLS configuration", func() {
							err := monitoring.AssertServiceMTLSEnabled(sharedClients, svc)
							Expect(err).NotTo(HaveOccurred())
						})

						It("Pod for "+svc.ServiceName+" has mTLS env vars and volumes", func() {
							err := monitoring.AssertPodMTLSEnabled(sharedClients, svc)
							Expect(err).NotTo(HaveOccurred())
						})

						It("Prometheus scrape health for "+svc.ServiceName+" reports up=1", func() {
							err := monitoring.VerifyHealthStatusMetric(sharedClients, monitoring.TargetService{
								Job:           svc.ServiceName,
								ExpectedValue: "1",
							})
							Expect(err).NotTo(HaveOccurred(),
								"Health status metric check failed for job: %s", svc.ServiceName)
						})
					}

					monitorCfg := monitoring.MTLSComponentConfig{
						ServiceMonitorName: comp.serviceMonitor,
						ServiceName:        comp.services[0].ServiceName,
						Namespace:          config.TargetNamespace,
						ComponentName:      comp.component,
					}

					It("ServiceMonitor "+comp.serviceMonitor+" has HTTPS scheme and tlsConfig", func() {
						err := monitoring.AssertServiceMonitorMTLSEnabled(monitorCfg)
						Expect(err).NotTo(HaveOccurred())
					})
				})
			}

			// TLS handshake validation: with-cert succeeds, without-cert is rejected
			It("TLS handshake succeeds with client cert and fails without", func() {
				cfg := monitoring.MTLSComponentConfig{
					ComponentName: "TektonPipeline",
					ServiceName:   "tekton-pipelines-controller",
					Namespace:     config.TargetNamespace,
					HTTPPortName:  "http-metrics",
					HTTPSPortName: "https-metrics",
				}
				err := monitoring.AssertTLSHandshake(sharedClients, cfg)
				Expect(err).NotTo(HaveOccurred())
			})

			// PAC has an extra ServiceMonitor
			It("ServiceMonitor "+pacExtraServiceMonitor+" has HTTPS scheme and tlsConfig", func() {
				cfg := monitoring.MTLSComponentConfig{
					ServiceMonitorName: pacExtraServiceMonitor,
					ServiceName:        "pipelines-as-code-controller",
					Namespace:          config.TargetNamespace,
					ComponentName:      "OpenShiftPipelinesAsCode",
				}
				err := monitoring.AssertServiceMonitorMTLSEnabled(cfg)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		// ── mTLS disabled assertions ────────────────────────────────────────

		Describe("mTLS disabled", func() {
			BeforeAll(func() {
				Expect(monitoring.SetEnableMetricsMTLS(false)).To(Succeed())
				err := monitoring.WaitForTektonConfigReady(sharedClients)
				Expect(err).NotTo(HaveOccurred(), "TektonConfig did not reach Ready after disabling mTLS")
			})

			for _, comp := range mtlsComponents {
				Context(comp.component, func() {
					for _, svc := range comp.services {
						svc.Namespace = config.TargetNamespace
						svc.ComponentName = comp.component

						It("Service "+svc.ServiceName+" has plain HTTP configuration", func() {
							err := monitoring.AssertServiceMTLSDisabled(sharedClients, svc)
							Expect(err).NotTo(HaveOccurred())
						})

						It("Pod for "+svc.ServiceName+" has no mTLS env vars", func() {
							err := monitoring.AssertPodMTLSDisabled(sharedClients, svc)
							Expect(err).NotTo(HaveOccurred())
						})

						It("Prometheus scrape health for "+svc.ServiceName+" reports up=1 over plain HTTP", func() {
							err := monitoring.VerifyHealthStatusMetric(sharedClients, monitoring.TargetService{
								Job:           svc.ServiceName,
								ExpectedValue: "1",
							})
							Expect(err).NotTo(HaveOccurred(),
								"Health status metric check failed for job: %s", svc.ServiceName)
						})
					}

					monitorCfg := monitoring.MTLSComponentConfig{
						ServiceMonitorName: comp.serviceMonitor,
						Namespace:          config.TargetNamespace,
						ComponentName:      comp.component,
					}

					It("ServiceMonitor "+comp.serviceMonitor+" has plain HTTP scheme", func() {
						err := monitoring.AssertServiceMonitorMTLSDisabled(monitorCfg)
						Expect(err).NotTo(HaveOccurred())
					})
				})
			}

			It("ServiceMonitor "+pacExtraServiceMonitor+" has plain HTTP scheme", func() {
				cfg := monitoring.MTLSComponentConfig{
					ServiceMonitorName: pacExtraServiceMonitor,
					Namespace:          config.TargetNamespace,
					ComponentName:      "OpenShiftPipelinesAsCode",
				}
				err := monitoring.AssertServiceMonitorMTLSDisabled(cfg)
				Expect(err).NotTo(HaveOccurred())
			})

			// Plain HTTP handshake validation: metrics endpoint is accessible without TLS
			It("plain HTTP handshake succeeds without client cert", func() {
				cfg := monitoring.MTLSComponentConfig{
					ComponentName: "TektonPipeline",
					ServiceName:   "tekton-pipelines-controller",
					Namespace:     config.TargetNamespace,
					HTTPPortName:  "http-metrics",
					HTTPSPortName: "https-metrics",
				}
				err := monitoring.AssertPlainHTTPHandshake(sharedClients, cfg)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		// ── Negative test: tekton-results-api stays plain HTTP always ────────

		Describe("tekton-results-api stays plain HTTP regardless of enableMetricsMTLS", func() {

			resultsAPICfg := monitoring.MTLSComponentConfig{
				ComponentName:      "TektonResult-API",
				ServiceName:        "tekton-results-api",
				ServiceMonitorName: "openshift-results-api-monitor",
				Namespace:          config.TargetNamespace,
				HTTPPortName:       "http-metrics",
				HTTPSPortName:      "https-metrics",
			}

			It("stays plain HTTP when mTLS is enabled", func() {
				Expect(monitoring.SetEnableMetricsMTLS(true)).To(Succeed())
				err := monitoring.WaitForTektonConfigReady(sharedClients)
				Expect(err).NotTo(HaveOccurred(), "TektonConfig did not reach Ready")

				By("Service port stays plain")
				err = monitoring.AssertServiceMTLSDisabled(sharedClients, resultsAPICfg)
				Expect(err).NotTo(HaveOccurred())

				By("ServiceMonitor stays plain HTTP")
				err = monitoring.AssertServiceMonitorMTLSDisabled(resultsAPICfg)
				Expect(err).NotTo(HaveOccurred())

				By("Prometheus scrape health reports up=1 over plain HTTP")
				err = monitoring.VerifyHealthStatusMetric(sharedClients, monitoring.TargetService{
					Job:           "tekton-results-api",
					ExpectedValue: "1",
				})
				Expect(err).NotTo(HaveOccurred())
			})

			It("stays plain HTTP when mTLS is disabled", func() {
				Expect(monitoring.SetEnableMetricsMTLS(false)).To(Succeed())
				err := monitoring.WaitForTektonConfigReady(sharedClients)
				Expect(err).NotTo(HaveOccurred(), "TektonConfig did not reach Ready")

				By("Service port stays plain")
				err = monitoring.AssertServiceMTLSDisabled(sharedClients, resultsAPICfg)
				Expect(err).NotTo(HaveOccurred())

				By("ServiceMonitor stays plain HTTP")
				err = monitoring.AssertServiceMonitorMTLSDisabled(resultsAPICfg)
				Expect(err).NotTo(HaveOccurred())

				By("Prometheus scrape health reports up=1 over plain HTTP")
				err = monitoring.VerifyHealthStatusMetric(sharedClients, monitoring.TargetService{
					Job:           "tekton-results-api",
					ExpectedValue: "1",
				})
				Expect(err).NotTo(HaveOccurred())
			})
		})

		// ── Toggle idempotency ──────────────────────────────────────────────

		Describe("toggle idempotency (enable → disable → enable)", func() {

			It("cycles the flag without stuck InstallerSets and TektonConfig stays Ready", func() {
				By("Enable mTLS (first time)")
				Expect(monitoring.SetEnableMetricsMTLS(true)).To(Succeed())
				err := monitoring.WaitForTektonConfigReady(sharedClients)
				Expect(err).NotTo(HaveOccurred(), "TektonConfig not Ready after enable #1")
				err = monitoring.AssertNoStuckInstallerSets()
				Expect(err).NotTo(HaveOccurred(), "Stuck InstallerSets after enable #1")

				By("Disable mTLS")
				Expect(monitoring.SetEnableMetricsMTLS(false)).To(Succeed())
				err = monitoring.WaitForTektonConfigReady(sharedClients)
				Expect(err).NotTo(HaveOccurred(), "TektonConfig not Ready after disable")
				err = monitoring.AssertNoStuckInstallerSets()
				Expect(err).NotTo(HaveOccurred(), "Stuck InstallerSets after disable")

				By("Re-enable mTLS (second time)")
				Expect(monitoring.SetEnableMetricsMTLS(true)).To(Succeed())
				err = monitoring.WaitForTektonConfigReady(sharedClients)
				Expect(err).NotTo(HaveOccurred(), "TektonConfig not Ready after enable #2")
				err = monitoring.AssertNoStuckInstallerSets()
				Expect(err).NotTo(HaveOccurred(), "Stuck InstallerSets after enable #2")

				By("Verify mTLS is correctly configured after re-enable")
				// Spot-check the pipelines controller Service
				svcCfg := monitoring.MTLSComponentConfig{
					ServiceName:   "tekton-pipelines-controller",
					Namespace:     config.TargetNamespace,
					HTTPPortName:  "http-metrics",
					HTTPSPortName: "https-metrics",
				}
				err = monitoring.AssertServiceMTLSEnabled(sharedClients, svcCfg)
				Expect(err).NotTo(HaveOccurred(),
					"tekton-pipelines-controller Service should have mTLS after re-enable")
			})
		})
	})
