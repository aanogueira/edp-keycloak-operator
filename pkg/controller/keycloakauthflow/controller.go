package keycloakauthflow

import (
	"context"
	"reflect"
	"time"

	k8sErrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/epam/edp-keycloak-operator/pkg/apis/v1/v1alpha1"
	keycloakApi "github.com/epam/edp-keycloak-operator/pkg/apis/v1/v1alpha1"
	"github.com/epam/edp-keycloak-operator/pkg/client/keycloak"
	"github.com/epam/edp-keycloak-operator/pkg/client/keycloak/adapter"
	"github.com/epam/edp-keycloak-operator/pkg/controller/helper"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const finalizerName = "keycloak.authflow.operator.finalizer.name"

type Helper interface {
	SetFailureCount(fc helper.FailureCountable) time.Duration
	UpdateStatus(obj client.Object) error
	TryToDelete(ctx context.Context, obj helper.Deletable, terminator helper.Terminator, finalizer string) (isDeleted bool, resultErr error)
	CreateKeycloakClientForRealm(realm *v1alpha1.KeycloakRealm, log logr.Logger) (keycloak.Client, error)
	GetOrCreateRealmOwnerRef(object helper.RealmChild, objectMeta v1.ObjectMeta) (*v1alpha1.KeycloakRealm, error)
}

type Reconcile struct {
	client client.Client
	scheme *runtime.Scheme
	helper Helper
	log    logr.Logger
}

func NewReconcile(client client.Client, scheme *runtime.Scheme, log logr.Logger) *Reconcile {
	return &Reconcile{
		client: client,
		scheme: scheme,
		helper: helper.MakeHelper(client, scheme),
		log:    log.WithName("keycloak-auth-flow"),
	}
}

func (r *Reconcile) SetupWithManager(mgr ctrl.Manager) error {
	pred := predicate.Funcs{
		UpdateFunc: isSpecUpdated,
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&keycloakApi.KeycloakAuthFlow{}, builder.WithPredicates(pred)).
		Complete(r)
}

func isSpecUpdated(e event.UpdateEvent) bool {
	oo := e.ObjectOld.(*keycloakApi.KeycloakAuthFlow)
	no := e.ObjectNew.(*keycloakApi.KeycloakAuthFlow)

	return !reflect.DeepEqual(oo.Spec, no.Spec) ||
		(oo.GetDeletionTimestamp().IsZero() && !no.GetDeletionTimestamp().IsZero())
}

func (r *Reconcile) Reconcile(ctx context.Context, request reconcile.Request) (result reconcile.Result,
	resultErr error) {
	log := r.log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	log.Info("Reconciling KeycloakAuthFlow")

	var instance keycloakApi.KeycloakAuthFlow
	if err := r.client.Get(ctx, request.NamespacedName, &instance); err != nil {
		if k8sErrors.IsNotFound(err) {
			return
		}

		resultErr = errors.Wrap(err, "unable to get keycloak auth flow from k8s")
		return
	}

	if err := r.tryReconcile(ctx, &instance); err != nil {
		instance.Status.Value = err.Error()
		result.RequeueAfter = r.helper.SetFailureCount(&instance)
		log.Error(err, "an error has occurred while handling keycloak auth flow", "name",
			request.Name)
	} else {
		helper.SetSuccessStatus(&instance)
	}

	if err := r.helper.UpdateStatus(&instance); err != nil {
		resultErr = err
	}

	log.Info("Reconciling KeycloakAuthFlow done.")

	return
}

func (r *Reconcile) tryReconcile(ctx context.Context, keycloakAuthFlow *keycloakApi.KeycloakAuthFlow) error {
	realm, err := r.helper.GetOrCreateRealmOwnerRef(keycloakAuthFlow, keycloakAuthFlow.ObjectMeta)
	if err != nil {
		return errors.Wrap(err, "unable to get realm owner ref")
	}

	kClient, err := r.helper.CreateKeycloakClientForRealm(realm, r.log)
	if err != nil {
		return errors.Wrap(err, "unable to create keycloak client")
	}

	if err := kClient.SyncAuthFlow(realm.Spec.RealmName,
		authFlowSpecToAdapterAuthFlow(&keycloakAuthFlow.Spec)); err != nil {
		return errors.Wrap(err, "unable to sync auth flow")
	}

	if _, err := r.helper.TryToDelete(ctx, keycloakAuthFlow,
		makeTerminator(realm.Spec.RealmName, keycloakAuthFlow.Spec.Alias, kClient,
			r.log.WithName("auth-flow-term")), finalizerName); err != nil {
		return errors.Wrap(err, "unable to tryToDelete auth flow")
	}

	return nil
}

func authFlowSpecToAdapterAuthFlow(spec *keycloakApi.KeycloakAuthFlowSpec) *adapter.KeycloakAuthFlow {
	flow := adapter.KeycloakAuthFlow{
		Alias:                    spec.Alias,
		Description:              spec.Description,
		BuiltIn:                  spec.BuiltIn,
		ProviderID:               spec.ProviderID,
		TopLevel:                 spec.TopLevel,
		AuthenticationExecutions: make([]adapter.AuthenticationExecution, 0, len(spec.AuthenticationExecutions)),
	}

	for _, ae := range spec.AuthenticationExecutions {
		exec := adapter.AuthenticationExecution{
			Authenticator:    ae.Authenticator,
			Requirement:      ae.Requirement,
			Priority:         ae.Priority,
			AutheticatorFlow: ae.AuthenticatorFlow,
		}

		if ae.AuthenticatorConfig != nil {
			exec.AuthenticatorConfig = &adapter.AuthenticatorConfig{
				Alias:  ae.AuthenticatorConfig.Alias,
				Config: ae.AuthenticatorConfig.Config,
			}
		}

		flow.AuthenticationExecutions = append(flow.AuthenticationExecutions, exec)
	}

	return &flow
}