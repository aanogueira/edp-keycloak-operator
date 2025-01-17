package keycloakclientscope

import (
	"context"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	keycloakApi "github.com/epam/edp-keycloak-operator/api/v1/v1"
	"github.com/epam/edp-keycloak-operator/controllers/helper"
	"github.com/epam/edp-keycloak-operator/pkg/client/keycloak/adapter"
	"github.com/epam/edp-keycloak-operator/pkg/client/keycloak/mock"
)

func getTestClientScope(realmName string) *keycloakApi.KeycloakClientScope {
	return &keycloakApi.KeycloakClientScope{
		ObjectMeta: metav1.ObjectMeta{
			Name: "scope1",
		},
		TypeMeta: metav1.TypeMeta{Kind: "KeycloakClientScope", APIVersion: "v1.edp.epam.com/v1"},
		Spec: keycloakApi.KeycloakClientScopeSpec{
			Name:  "scope1name",
			Realm: realmName,
		},
	}
}

func TestReconcile_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(keycloakApi.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))

	ns := "security"
	keycloak := keycloakApi.Keycloak{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: ns},
		Spec: keycloakApi.KeycloakSpec{
			Secret: "keycloak-secret",
		},
		Status: keycloakApi.KeycloakStatus{Connected: true}}
	realm := keycloakApi.KeycloakRealm{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: ns,
		OwnerReferences: []metav1.OwnerReference{{Name: "test", Kind: "Keycloak"}}},
		Spec: keycloakApi.KeycloakRealmSpec{RealmName: "ns.test"}}

	clientScope := getTestClientScope(realm.Name)

	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(clientScope, &realm, &keycloak).Build()
	kClient := new(adapter.Mock)
	kClient.On("GetClientScope", clientScope.Spec.Name, realm.Spec.RealmName).
		Return(nil, adapter.NotFoundError("not found"))
	kClient.On("CreateClientScope", realm.Spec.RealmName, &adapter.ClientScope{
		Name:            clientScope.Spec.Name,
		ProtocolMappers: []adapter.ProtocolMapper{},
	}).
		Return("scope12", nil)

	logger := mock.NewLogr()
	h := helper.Mock{}
	h.On("CreateKeycloakClientForRealm", &realm).Return(kClient, nil)
	h.On("GetOrCreateRealmOwnerRef", clientScope, &clientScope.ObjectMeta).Return(&realm, nil)

	updatedClientScopeWithID := getTestClientScope(realm.Name)
	updatedClientScopeWithID.Status.ID = "scope12"
	updatedClientScopeWithID.ResourceVersion = "999"

	updatedClientScopeWithStatus := getTestClientScope(realm.Name)
	updatedClientScopeWithStatus.Status.ID = "scope12"
	updatedClientScopeWithStatus.ResourceVersion = "999"
	updatedClientScopeWithStatus.Status.Value = helper.StatusOK

	h.On("UpdateStatus", updatedClientScopeWithStatus).Return(nil)

	h.On("TryToDelete", updatedClientScopeWithID,
		makeTerminator(kClient, realm.Spec.RealmName, "scope12", logger), finalizerName).
		Return(true, nil)

	rkr := Reconcile{
		log:                     logger,
		client:                  client,
		helper:                  &h,
		successReconcileTimeout: time.Hour,
	}

	res, err := rkr.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      clientScope.Name,
			Namespace: clientScope.Namespace,
		}})
	require.NoError(t, err)

	if res.RequeueAfter != rkr.successReconcileTimeout {
		t.Fatal("success reconcile timeout is not set")
	}
}

func TestSpecIsUpdated(t *testing.T) {
	cs := getTestClientScope("test")

	if isSpecUpdated(event.UpdateEvent{
		ObjectNew: cs,
		ObjectOld: cs,
	}) {
		t.Fatal("spec must not be updated")
	}
}

func TestNewReconcile(t *testing.T) {
	var (
		scheme = runtime.NewScheme()
		client = fake.NewClientBuilder().WithScheme(scheme).Build()
		hlp    helper.Mock
	)

	rec := NewReconcile(client, mock.NewLogr(), &hlp)
	if rec == nil {
		t.Fatal("reconciler is not inited")
	}
}

func TestReconcile_Reconcile_NotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(keycloakApi.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	logger := mock.NewLogr()
	rec := NewReconcile(client, logger, &helper.Mock{})

	_, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: "foo", Namespace: "bar"}})
	require.NoError(t, err)

	if _, ok := logger.GetSink().(*mock.Logger).InfoMessages()["instance not found"]; !ok {
		t.Fatal("no info messages is logged")
	}
}

func TestConvertProtocolMappers(t *testing.T) {
	mappers := convertProtocolMappers([]keycloakApi.ProtocolMapper{
		{Name: "test1"},
	})

	if len(mappers) == 0 {
		t.Fatal("protocol mappers is not converted")
	}

	if mappers[0].Name != "test1" {
		t.Fatal("protocol mappers converted wrongly")
	}
}

func TestSyncClientScope(t *testing.T) {
	kClient := new(adapter.Mock)
	realm := keycloakApi.KeycloakRealm{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "ns",
		OwnerReferences: []metav1.OwnerReference{{Name: "test", Kind: "Keycloak"}}},
		Spec: keycloakApi.KeycloakRealmSpec{RealmName: "ns.test"}}
	instance := getTestClientScope(realm.Name)
	scopeID := "scopeID1"

	kClient.On("GetClientScope", instance.Spec.Name, realm.Spec.RealmName).Return(&adapter.ClientScope{
		ID: scopeID,
	}, nil)
	kClient.On("UpdateClientScope", realm.Spec.RealmName, scopeID, &adapter.ClientScope{
		Name:            instance.Spec.Name,
		ProtocolMappers: []adapter.ProtocolMapper{},
	}).Return(nil)

	_, err := syncClientScope(context.Background(), instance, &realm, kClient)
	require.NoError(t, err)
}

func TestReconcile_Reconcile_FailureNoRealm(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(keycloakApi.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))

	instance := getTestClientScope("test")

	client := fake.NewClientBuilder().WithRuntimeObjects(instance).WithScheme(scheme).Build()
	logger := mock.NewLogr()
	rec := NewReconcile(client, logger, helper.MakeHelper(client, scheme, logger))

	if _, err := rec.Reconcile(context.Background(),
		reconcile.Request{NamespacedName: types.NamespacedName{Name: instance.Name, Namespace: instance.Namespace}}); err != nil {
		t.Fatalf("%+v", err)
	}

	loggerSink, ok := logger.GetSink().(*mock.Logger)
	require.True(t, ok, "wrong logger type")
	require.Error(t, loggerSink.LastError())
	assert.Contains(t, loggerSink.LastError().Error(), "unable to get realm owner ref")
}
func TestReconcile_Reconcile_FailureNoClientForRealm(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(keycloakApi.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))

	realm := keycloakApi.KeycloakRealm{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "ns",
		OwnerReferences: []metav1.OwnerReference{{Name: "test", Kind: "Keycloak"}}},
		Spec: keycloakApi.KeycloakRealmSpec{RealmName: "ns.test"}}
	clientScope := getTestClientScope(realm.Name)

	client := fake.NewClientBuilder().WithRuntimeObjects(clientScope, &realm).WithScheme(scheme).Build()
	logger := mock.NewLogr()
	h := helper.Mock{}

	rec := NewReconcile(client, logger, &h)

	h.On("GetOrCreateRealmOwnerRef", clientScope, &clientScope.ObjectMeta).Return(&realm, nil)
	h.On("CreateKeycloakClientForRealm", &realm).
		Return(nil, errors.New("fatal"))

	updatedClientScope := getTestClientScope(realm.Name)
	updatedClientScope.Status.Value = "unable to create keycloak client: fatal"
	updatedClientScope.ResourceVersion = "999"

	h.On("SetFailureCount", updatedClientScope).Return(time.Minute)
	h.On("UpdateStatus", updatedClientScope).Return(nil)

	rec.helper = &h

	if _, err := rec.Reconcile(context.Background(),
		reconcile.Request{NamespacedName: types.NamespacedName{Name: clientScope.Name, Namespace: clientScope.Namespace}}); err != nil {
		t.Fatalf("%+v", err)
	}

	loggerSink, ok := logger.GetSink().(*mock.Logger)
	require.True(t, ok, "wrong logger type")
	require.Error(t, loggerSink.LastError())
	assert.Contains(t, loggerSink.LastError().Error(), "unable to create keycloak client")
}
