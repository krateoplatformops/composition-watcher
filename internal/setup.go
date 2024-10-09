package internal

import (
	"github.com/krateoplatformops/provider-runtime/pkg/controller"
	ctrl "sigs.k8s.io/controller-runtime"

	watcher "github.com/krateoplatformops/composition-watcher/internal/controller"
	informerHelper "github.com/krateoplatformops/composition-watcher/internal/helpers/informer"
)

// Setup creates all controllers with the supplied logger and adds them to
// the supplied manager.
func Setup(mgr ctrl.Manager, o controller.Options, inf *informerHelper.CompositionInformer) error {
	for _, setup := range []func(ctrl.Manager, controller.Options, *informerHelper.CompositionInformer) error{
		watcher.Setup,
	} {
		if err := setup(mgr, o, inf); err != nil {
			return err
		}
	}
	return nil
}
