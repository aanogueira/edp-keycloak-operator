package helper

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-resty/resty/v2"
	"github.com/jarcoal/httpmock"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v13 "github.com/epam/edp-keycloak-operator/api/v1/v1"
	"github.com/epam/edp-keycloak-operator/pkg/client/keycloak/mock"
)

func TestHelper_GetOrCreateRealmOwnerRef(t *testing.T) {
	mc := K8SClientMock{}

	sch := runtime.NewScheme()
	utilruntime.Must(v13.AddToScheme(sch))

	helper := MakeHelper(&mc, sch, mock.NewLogr())

	kcGroup := v13.KeycloakRealmGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			OwnerReferences: []metav1.OwnerReference{
				{
					Name: "foo",
					Kind: "KeycloakRealm",
				},
			},
		},
	}

	mc.On("Get", types.NamespacedName{
		Namespace: "test",
		Name:      "foo",
	}, &v13.KeycloakRealm{}).Return(nil)

	_, err := helper.GetOrCreateRealmOwnerRef(&kcGroup, &kcGroup.ObjectMeta)
	require.NoError(t, err)

	kcGroup = v13.KeycloakRealmGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
		},
		Spec: v13.KeycloakRealmGroupSpec{
			Realm: "foo13",
		},
	}

	mc.On("Get", types.NamespacedName{
		Namespace: "test",
		Name:      "foo13",
	}, &v13.KeycloakRealm{}).Return(nil)

	_, err = helper.GetOrCreateRealmOwnerRef(&kcGroup, &kcGroup.ObjectMeta)
	require.NoError(t, err)
}

func TestHelper_GetOrCreateRealmOwnerRef_Failure(t *testing.T) {
	mc := K8SClientMock{}

	sch := runtime.NewScheme()
	utilruntime.Must(v13.AddToScheme(sch))

	helper := MakeHelper(&mc, sch, mock.NewLogr())

	kcGroup := v13.KeycloakRealmGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			OwnerReferences: []metav1.OwnerReference{
				{
					Name: "foo",
					Kind: "KeycloakRealm",
				},
			},
		},
	}

	mockErr := errors.New("mock error")

	mc.On("Get", types.NamespacedName{
		Namespace: "test",
		Name:      "foo",
	}, &v13.KeycloakRealm{}).Return(mockErr)

	_, err := helper.GetOrCreateRealmOwnerRef(&kcGroup, &kcGroup.ObjectMeta)
	if err == nil {
		t.Fatal("no error on k8s client get fatal")
	}

	assert.ErrorIs(t, err, mockErr)

	kcGroup = v13.KeycloakRealmGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
		},
		Spec: v13.KeycloakRealmGroupSpec{Realm: "main123"},
	}

	mc.On("Get", types.NamespacedName{
		Namespace: "test",
		Name:      "main123",
	}, &v13.KeycloakRealm{}).Return(mockErr)

	_, err = helper.GetOrCreateRealmOwnerRef(&kcGroup, &kcGroup.ObjectMeta)
	if err == nil {
		t.Fatal("no error on k8s client get fatal")
	}

	assert.ErrorIs(t, err, mockErr)
}

func TestHelper_GetOrCreateKeycloakOwnerRef(t *testing.T) {
	mc := K8SClientMock{}

	sch := runtime.NewScheme()
	utilruntime.Must(v13.AddToScheme(sch))

	helper := MakeHelper(&mc, sch, mock.NewLogr())

	realm := v13.KeycloakRealm{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			OwnerReferences: []metav1.OwnerReference{
				{
					Name: "foo",
					Kind: "Keycloak",
				},
			},
		},
	}

	mc.On("Get", types.NamespacedName{
		Namespace: "test",
		Name:      "foo",
	}, &v13.Keycloak{}).Return(nil)

	_, err := helper.GetOrCreateKeycloakOwnerRef(&realm)
	require.NoError(t, err)

	realm = v13.KeycloakRealm{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
		},

		Spec: v13.KeycloakRealmSpec{
			KeycloakOwner: "test321",
		},
	}

	mc.On("Get", types.NamespacedName{
		Namespace: "test",
		Name:      "test321",
	}, &v13.Keycloak{}).Return(nil)

	_, err = helper.GetOrCreateKeycloakOwnerRef(&realm)
	require.NoError(t, err)
}

func TestHelper_GetOrCreateKeycloakOwnerRef_Failure(t *testing.T) {
	mc := K8SClientMock{}

	sch := runtime.NewScheme()
	utilruntime.Must(v13.AddToScheme(sch))

	helper := MakeHelper(&mc, sch, mock.NewLogr())

	realm := v13.KeycloakRealm{}

	_, err := helper.GetOrCreateKeycloakOwnerRef(&realm)
	if err == nil {
		t.Fatal("no error on empty owner reference and spec")
	}

	if errors.Cause(err).Error() != "keycloak owner is not specified neither in ownerReference nor in spec for realm " {
		t.Log(errors.Cause(err).Error())
		t.Fatal("wrong error message returned")
	}

	realm = v13.KeycloakRealm{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			OwnerReferences: []metav1.OwnerReference{
				{
					Name: "foo",
					Kind: "Deployment",
				},
			},
		},
	}

	_, err = helper.GetOrCreateKeycloakOwnerRef(&realm)
	if err == nil {
		t.Fatal("no error on empty owner reference and spec")
	}

	if errors.Cause(err).Error() != "keycloak owner is not specified neither in ownerReference nor in spec for realm " {
		t.Log(errors.Cause(err).Error())
		t.Fatal("wrong error message returned")
	}

	realm = v13.KeycloakRealm{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			OwnerReferences: []metav1.OwnerReference{
				{
					Name: "foo",
					Kind: "Deployment",
				},
			},
		},
		Spec: v13.KeycloakRealmSpec{
			KeycloakOwner: "testSpec",
		},
	}

	mockErr := errors.New("fatal")
	mc.On("Get", types.NamespacedName{
		Namespace: "test",
		Name:      "testSpec",
	}, &v13.Keycloak{}).Return(mockErr)

	_, err = helper.GetOrCreateKeycloakOwnerRef(&realm)
	if err == nil {
		t.Fatal("no error on k8s client get fatal")
	}

	assert.ErrorIs(t, err, mockErr)

	realm = v13.KeycloakRealm{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			OwnerReferences: []metav1.OwnerReference{
				{
					Name: "testOwnerReference",
					Kind: "Keycloak",
				},
			},
		},
		Spec: v13.KeycloakRealmSpec{
			KeycloakOwner: "testSpec",
		},
	}

	mc.On("Get", types.NamespacedName{
		Namespace: "test",
		Name:      "testOwnerReference",
	}, &v13.Keycloak{}).Return(mockErr)

	_, err = helper.GetOrCreateKeycloakOwnerRef(&realm)
	if err == nil {
		t.Fatal("no error on k8s client get fatal")
	}

	assert.ErrorIs(t, err, mockErr)
}

func TestMakeHelper(t *testing.T) {
	rCl := resty.New()
	httpmock.ActivateNonDefault(rCl.GetClient())
	httpmock.RegisterResponder("POST", "/k-url/realms/master/protocol/openid-connect/token",
		httpmock.NewStringResponder(200, "{}"))

	logger := mock.NewLogr()
	h := MakeHelper(nil, nil, logger)
	_, err := h.adapterBuilder(context.Background(), "k-url", "foo", "bar",
		v13.KeycloakAdminTypeServiceAccount, logger, rCl)
	require.NoError(t, err)
}

func TestHelper_CreateKeycloakClient(t *testing.T) {
	mc := K8SClientMock{}

	utilruntime.Must(v13.AddToScheme(scheme.Scheme))
	helper := MakeHelper(&mc, scheme.Scheme, mock.NewLogr())
	realm := v13.KeycloakRealm{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
			OwnerReferences: []metav1.OwnerReference{
				{
					Name: "testOwnerReference",
					Kind: "Keycloak",
				},
			},
		},
	}

	kc := v13.Keycloak{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: "testOwnerReference"},
		Status:     v13.KeycloakStatus{Connected: true},
		Spec:       v13.KeycloakSpec{Secret: "ss1"},
	}

	fakeCl := fake.NewClientBuilder().WithRuntimeObjects(&kc).Build()

	mc.On("Get", types.NamespacedName{
		Namespace: "test",
		Name:      "testOwnerReference",
	}, &v13.Keycloak{}).Return(fakeCl)

	mc.On("Get", types.NamespacedName{
		Namespace: "test",
		Name:      kc.Spec.Secret,
	}, &v1.Secret{}).Return(nil)

	mc.On("Get", types.NamespacedName{
		Namespace: "test",
		Name:      "kc-token-testOwnerReference",
	}, &v1.Secret{}).Return(&k8sErrors.StatusError{ErrStatus: metav1.Status{
		Status:  metav1.StatusFailure,
		Code:    http.StatusNotFound,
		Reason:  metav1.StatusReasonNotFound,
		Message: "not found",
	}})

	_, err := helper.CreateKeycloakClientForRealm(context.Background(), &realm)
	if err == nil {
		t.Fatal("no error on trying to connect to keycloak")
	}

	if !strings.Contains(err.Error(), "could not get token") {
		t.Fatalf("wrong error returned: %s", err.Error())
	}
}

type testTerminator struct {
	err error
	log logr.Logger
}

func (t *testTerminator) DeleteResource(ctx context.Context) error {
	return t.err
}
func (t *testTerminator) GetLogger() logr.Logger {
	return t.log
}

func TestHelper_TryToDelete(t *testing.T) {
	logger := mock.NewLogr()

	term := testTerminator{
		log: logger,
	}
	secret := v1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "test-secret1"}}
	fakeClient := fake.NewClientBuilder().WithRuntimeObjects(&secret).Build()
	h := Helper{client: fakeClient}

	_, err := h.TryToDelete(context.Background(), &secret, &term, "fin")
	require.NoError(t, err)

	term.err = errors.New("delete resource fatal")
	secret.DeletionTimestamp = &metav1.Time{Time: time.Now()}

	_, err = h.TryToDelete(context.Background(), &secret, &term, "fin")
	require.Error(t, err)

	if err.Error() != "error during keycloak resource deletion: delete resource fatal" {
		t.Fatalf("wrong error returned: %s", err.Error())
	}
}
