package keycloakrealmcomponent

import (
	"context"
	"reflect"
	"time"

	keycloakApi "github.com/epam/edp-keycloak-operator/pkg/apis/v1/v1alpha1"
	"github.com/epam/edp-keycloak-operator/pkg/client/keycloak"
	"github.com/epam/edp-keycloak-operator/pkg/client/keycloak/adapter"
	"github.com/epam/edp-keycloak-operator/pkg/controller/helper"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const finalizerName = "keycloak.realmcomponent.operator.finalizer.name"

type Helper interface {
	SetFailureCount(fc helper.FailureCountable) time.Duration
	UpdateStatus(obj client.Object) error
	GetOrCreateRealmOwnerRef(object helper.RealmChild, objectMeta v1.ObjectMeta) (*keycloakApi.KeycloakRealm, error)
	CreateKeycloakClientForRealm(ctx context.Context, realm *keycloakApi.KeycloakRealm) (keycloak.Client, error)
	TryToDelete(ctx context.Context, obj helper.Deletable, terminator helper.Terminator, finalizer string) (isDeleted bool, resultErr error)
}

type Reconcile struct {
	client                  client.Client
	log                     logr.Logger
	helper                  Helper
	successReconcileTimeout time.Duration
}

func NewReconcile(client client.Client, log logr.Logger, helper Helper) *Reconcile {
	return &Reconcile{
		client: client,
		helper: helper,
		log:    log.WithName("keycloak-realm-component"),
	}
}

func (r *Reconcile) SetupWithManager(mgr ctrl.Manager, successReconcileTimeout time.Duration) error {
	r.successReconcileTimeout = successReconcileTimeout

	pred := predicate.Funcs{
		UpdateFunc: isSpecUpdated,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&keycloakApi.KeycloakRealmComponent{}, builder.WithPredicates(pred)).
		Complete(r)
}

func isSpecUpdated(e event.UpdateEvent) bool {
	oo := e.ObjectOld.(*keycloakApi.KeycloakRealmComponent)
	no := e.ObjectNew.(*keycloakApi.KeycloakRealmComponent)

	return !reflect.DeepEqual(oo.Spec, no.Spec) ||
		(oo.GetDeletionTimestamp().IsZero() && !no.GetDeletionTimestamp().IsZero())
}

func (r *Reconcile) Reconcile(ctx context.Context, request reconcile.Request) (result reconcile.Result, resultErr error) {
	log := r.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	log.Info("Reconciling KeycloakRealmComponent")

	var instance keycloakApi.KeycloakRealmComponent
	if err := r.client.Get(ctx, request.NamespacedName, &instance); err != nil {
		if k8sErrors.IsNotFound(err) {
			return
		}

		resultErr = errors.Wrap(err, "unable to get keycloak realm component from k8s")
		return
	}

	if err := r.tryReconcile(ctx, &instance); err != nil {
		instance.Status.Value = err.Error()
		result.RequeueAfter = r.helper.SetFailureCount(&instance)
		log.Error(err, "an error has occurred while handling keycloak realm component", "name",
			request.Name)
	} else {
		helper.SetSuccessStatus(&instance)
		result.RequeueAfter = r.successReconcileTimeout
	}

	if err := r.helper.UpdateStatus(&instance); err != nil {
		resultErr = errors.Wrap(err, "unable to update status")
	}

	return
}

func (r *Reconcile) tryReconcile(ctx context.Context, keycloakRealmComponent *keycloakApi.KeycloakRealmComponent) error {
	realm, err := r.helper.GetOrCreateRealmOwnerRef(keycloakRealmComponent, keycloakRealmComponent.ObjectMeta)
	if err != nil {
		return errors.Wrap(err, "unable to get realm owner ref")
	}

	kClient, err := r.helper.CreateKeycloakClientForRealm(ctx, realm)
	if err != nil {
		return errors.Wrap(err, "unable to create keycloak client")
	}

	keycloakComponent := createKeycloakComponentFromSpec(&keycloakRealmComponent.Spec)

	cmp, err := kClient.GetComponent(ctx, realm.Spec.RealmName, keycloakRealmComponent.Spec.Name)
	if err == nil {
		keycloakComponent.ID = cmp.ID

		if err := kClient.UpdateComponent(ctx, realm.Spec.RealmName, keycloakComponent); err != nil {
			return errors.Wrap(err, "unable to update component")
		}
	} else if adapter.IsErrNotFound(err) {
		if err := kClient.CreateComponent(ctx, realm.Spec.RealmName, keycloakComponent); err != nil {
			return errors.Wrap(err, "unable to create component")
		}
	} else {
		return errors.Wrap(err, "unable to get component, unexpected error")
	}

	if _, err := r.helper.TryToDelete(ctx, keycloakRealmComponent,
		makeTerminator(realm.Spec.RealmName, keycloakRealmComponent.Spec.Name, kClient,
			r.log.WithName("realm-component-term")),
		finalizerName); err != nil {
		return errors.Wrap(err, "unable to tryToDelete realm component")
	}

	return nil
}

func createKeycloakComponentFromSpec(spec *keycloakApi.KeycloakComponentSpec) *adapter.Component {
	return &adapter.Component{
		Name:         spec.Name,
		Config:       spec.Config,
		ProviderID:   spec.ProviderID,
		ProviderType: spec.ProviderType,
	}
}