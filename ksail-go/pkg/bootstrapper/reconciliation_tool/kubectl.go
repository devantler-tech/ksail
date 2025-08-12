package reconciliationtoolbootstrapper

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/internal/utils"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/yaml"
)

//go:embed assets/kubectl/apply-set-crd.yaml
var applySetCRDYAML []byte

//go:embed assets/kubectl/apply-set-cr.yaml
var applySetCRYAML []byte

type KubectlBootstrapper struct {
	kubeconfig string
	context    string
	timeout    time.Duration
}

func NewKubectlBootstrapper(kubeconfig, context string, timeout time.Duration) *KubectlBootstrapper {
	return &KubectlBootstrapper{
		kubeconfig: kubeconfig,
		context:    context,
		timeout:    timeout,
	}
}

// Install ensures the ApplySet CRD and its parent CR exist.
func (b *KubectlBootstrapper) Install() error {
	restConfigWrapper, err := b.buildRESTConfig()
	if err != nil {
		return err
	}

	// --- CRD ---
	apiExtClient, err := apiextensionsclient.NewForConfig(restConfigWrapper)
	if err != nil {
		return fmt.Errorf("failed to create apiextensions client: %w", err)
	}

	context, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()

	const crdName = "applysets.k8s.devantler.tech"
	_, err = apiExtClient.ApiextensionsV1().CustomResourceDefinitions().Get(context, crdName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		fmt.Println("► applying applysets crd 'applysets.k8s.devantler.tech'")
		if err := b.applyCRD(context, apiExtClient); err != nil {
			return err
		}
		if err := b.waitForCRDEstablished(context, apiExtClient, crdName); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	fmt.Println("✔ applysets crd 'applysets.k8s.devantler.tech' applied")

	// --- CR (ApplySet parent) ---
	dynClient, err := dynamic.NewForConfig(restConfigWrapper)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}
	gvr := schema.GroupVersionResource{Group: "k8s.devantler.tech", Version: "v1", Resource: "applysets"}
	const applySetName = "ksail"
	_, err = dynClient.Resource(gvr).Get(context, applySetName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		fmt.Println("► applying applysets cr 'ksail'")
		if err := b.applyApplySetCR(context, dynClient, gvr, applySetName); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("failed to get ApplySet CR: %w", err)
	}
	fmt.Println("✔ applysets cr 'ksail' applied")
	return nil
}

// Uninstall deletes the ApplySet CR then its CRD.
func (b *KubectlBootstrapper) Uninstall() error {
	config, err := b.buildRESTConfig()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
	defer cancel()

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}
	gvr := schema.GroupVersionResource{Group: "k8s.devantler.tech", Version: "v1", Resource: "applysets"}
	_ = dynClient.Resource(gvr).Delete(ctx, "ksail", metav1.DeleteOptions{}) // ignore errors (including NotFound)

	apiExtClient, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create apiextensions client: %w", err)
	}
	_ = apiExtClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, "applysets.k8s.devantler.tech", metav1.DeleteOptions{})
	return nil
}

// --- internals ---

func (b *KubectlBootstrapper) buildRESTConfig() (*rest.Config, error) {
	kubeconfigPath, _ := utils.ExpandPath(b.kubeconfig)
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	overrides := &clientcmd.ConfigOverrides{}
	if b.context != "" {
		overrides.CurrentContext = b.context
	}
	clientCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	restConfig, err := clientCfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build rest config: %w", err)
	}
	return restConfig, nil
}

// applyCRD creates the ApplySet CRD from embedded YAML.
func (b *KubectlBootstrapper) applyCRD(ctx context.Context, c *apiextensionsclient.Clientset) error {
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(applySetCRDYAML, &crd); err != nil {
		return fmt.Errorf("failed to unmarshal CRD yaml: %w", err)
	}
	// Attempt create; if already exists attempt update (could race).
	_, err := c.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, &crd, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := c.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, crd.Name, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to get existing CRD for update: %w", getErr)
		}
		crd.ResourceVersion = existing.ResourceVersion
		if _, uerr := c.ApiextensionsV1().CustomResourceDefinitions().Update(ctx, &crd, metav1.UpdateOptions{}); uerr != nil {
			return fmt.Errorf("failed to update CRD: %w", uerr)
		}
		return nil
	}
	return err
}

func (b *KubectlBootstrapper) waitForCRDEstablished(ctx context.Context, c *apiextensionsclient.Clientset, name string) error {
	// Poll every 500ms until Established=True or timeout
	pollCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	return wait.PollUntilContextTimeout(pollCtx, 500*time.Millisecond, b.timeout, true, func(ctx context.Context) (bool, error) {
		crd, err := c.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				return true, nil
			}
			if cond.Type == apiextensionsv1.NamesAccepted && cond.Status == apiextensionsv1.ConditionFalse && cond.Reason == "MultipleNamesNotAllowed" {
				return false, errors.New(cond.Message)
			}
		}
		return false, nil
	})
}

func (b *KubectlBootstrapper) applyApplySetCR(ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource, name string) error {
	var u unstructured.Unstructured
	if err := yaml.Unmarshal(applySetCRYAML, &u.Object); err != nil {
		return fmt.Errorf("failed to unmarshal ApplySet CR yaml: %w", err)
	}
	// Ensure GVK since yaml->map won't set it.
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "k8s.devantler.tech", Version: "v1", Kind: "ApplySet"})
	u.SetName(name)
	_, err := dyn.Resource(gvr).Create(ctx, &u, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := dyn.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("failed to get existing ApplySet: %w", getErr)
		}
		u.SetResourceVersion(existing.GetResourceVersion())
		if _, uerr := dyn.Resource(gvr).Update(ctx, &u, metav1.UpdateOptions{}); uerr != nil {
			return fmt.Errorf("failed to update ApplySet: %w", uerr)
		}
		return nil
	}
	return err
}
