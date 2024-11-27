// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package platform

import (
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/v8/channels/store/storetest"
	"github.com/mattermost/mattermost/server/v8/config"
	"github.com/mattermost/mattermost/server/v8/einterfaces/mocks"
)

func TestReadReplicaDisabledBasedOnLicense(t *testing.T) {
	cfg := model.Config{}
	cfg.SetDefaults()
	driverName := os.Getenv("MM_SQLSETTINGS_DRIVERNAME")
	if driverName == "" {
		driverName = model.DatabaseDriverPostgres
	}
	cfg.SqlSettings = *storetest.MakeSqlSettings(driverName, false)
	cfg.SqlSettings.DataSourceReplicas = []string{*cfg.SqlSettings.DataSource}
	cfg.SqlSettings.DataSourceSearchReplicas = []string{*cfg.SqlSettings.DataSource}

	t.Run("Read Replicas with no License", func(t *testing.T) {
		configStore := config.NewTestMemoryStore()
		_, _, err := configStore.Set(&cfg)
		require.NoError(t, err)
		ps, err := New(
			ServiceConfig{},
			ConfigStore(configStore),
		)
		require.NoError(t, err)
		require.Same(t, ps.sqlStore.GetMaster(), ps.sqlStore.GetReplica())
		require.Len(t, ps.Config().SqlSettings.DataSourceReplicas, 1)
	})

	t.Run("Read Replicas With License", func(t *testing.T) {
		configStore := config.NewTestMemoryStore()
		_, _, err := configStore.Set(&cfg)
		require.NoError(t, err)
		ps, err := New(
			ServiceConfig{},
			ConfigStore(configStore),
			func(ps *PlatformService) error {
				ps.licenseValue.Store(model.NewTestLicense())
				return nil
			},
		)
		require.NoError(t, err)
		require.NotSame(t, ps.sqlStore.GetMaster(), ps.sqlStore.GetReplica())
		require.Len(t, ps.Config().SqlSettings.DataSourceReplicas, 1)
	})

	t.Run("Search Replicas with no License", func(t *testing.T) {
		configStore := config.NewTestMemoryStore()
		_, _, err := configStore.Set(&cfg)
		require.NoError(t, err)
		ps, err := New(
			ServiceConfig{},
			ConfigStore(configStore),
		)
		require.NoError(t, err)
		require.Same(t, ps.sqlStore.GetMaster(), ps.sqlStore.GetSearchReplicaX())
		require.Len(t, ps.Config().SqlSettings.DataSourceSearchReplicas, 1)
	})

	t.Run("Search Replicas With License", func(t *testing.T) {
		configStore := config.NewTestMemoryStore()
		_, _, err := configStore.Set(&cfg)
		require.NoError(t, err)
		ps, err := New(
			ServiceConfig{},
			ConfigStore(configStore),
			func(ps *PlatformService) error {
				ps.licenseValue.Store(model.NewTestLicense())
				return nil
			},
		)
		require.NoError(t, err)
		require.NotSame(t, ps.sqlStore.GetMaster(), ps.sqlStore.GetSearchReplicaX())
		require.Len(t, ps.Config().SqlSettings.DataSourceSearchReplicas, 1)
	})
}

func TestMetrics(t *testing.T) {
	t.Run("ensure the metrics server is not started by default", func(t *testing.T) {
		th := Setup(t)
		defer th.TearDown()

		require.Nil(t, th.Service.metrics)
	})

	t.Run("ensure the metrics server is started", func(t *testing.T) {
		th := Setup(t, StartMetrics())
		defer th.TearDown()

		// there is no config listener for the metrics
		// we handle it on config save step
		cfg := th.Service.Config().Clone()
		cfg.MetricsSettings.Enable = model.NewPointer(true)
		_, _, appErr := th.Service.SaveConfig(cfg, false)
		require.Nil(t, appErr)

		require.NotNil(t, th.Service.metrics)
		metricsAddr := strings.Replace(th.Service.metrics.listenAddr, "[::]", "http://localhost", 1)
		metricsAddr = strings.Replace(metricsAddr, "127.0.0.1", "http://localhost", 1)

		resp, err := http.Get(metricsAddr)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)

		cfg.MetricsSettings.Enable = model.NewPointer(false)
		_, _, appErr = th.Service.SaveConfig(cfg, false)
		require.Nil(t, appErr)

		_, err = http.Get(metricsAddr)
		require.Error(t, err)
	})

	t.Run("ensure the metrics server is started with advanced metrics", func(t *testing.T) {
		th := Setup(t, StartMetrics())
		defer th.TearDown()

		mockMetricsImpl := &mocks.MetricsInterface{}
		mockMetricsImpl.On("Register").Return()

		th.Service.metricsIFace = mockMetricsImpl
		err := th.Service.resetMetrics()
		require.NoError(t, err)

		mockMetricsImpl.AssertExpectations(t)
	})

	t.Run("ensure advanced metrics have database metrics", func(t *testing.T) {
		mockMetricsImpl := &mocks.MetricsInterface{}
		mockMetricsImpl.On("Register").Return()
		mockMetricsImpl.On("ObserveStoreMethodDuration", mock.Anything, mock.Anything, mock.Anything).Return()
		mockMetricsImpl.On("RegisterDBCollector", mock.AnythingOfType("*sql.DB"), "master")

		th := Setup(t, StartMetrics(), func(ps *PlatformService) error {
			ps.metricsIFace = mockMetricsImpl
			return nil
		})
		defer th.TearDown()

		_ = th.CreateUserOrGuest(false)

		mockMetricsImpl.AssertExpectations(t)
	})
}

func TestShutdown(t *testing.T) {
	t.Run("should shutdown gracefully", func(t *testing.T) {
		th := Setup(t)
		rand.Seed(time.Now().UnixNano())

		// we create plenty of go routines to make sure we wait for all of them
		// to finish before shutting down
		for i := 0; i < 1000; i++ {
			th.Service.Go(func() {
				time.Sleep(time.Millisecond * time.Duration(rand.Intn(20)))
			})
		}

		err := th.Service.Shutdown()
		require.NoError(t, err)

		// assert that there are no more go routines running
		require.Zero(t, atomic.LoadInt32(&th.Service.goroutineCount))
	})
}

func TestSetTelemetryId(t *testing.T) {
	t.Run("ensure client config is regenerated after setting the telemetry id", func(t *testing.T) {
		th := Setup(t)
		defer th.TearDown()

		clientConfig := th.Service.LimitedClientConfig()
		require.Empty(t, clientConfig["DiagnosticId"])

		id := model.NewId()
		th.Service.SetTelemetryId(id)

		clientConfig = th.Service.LimitedClientConfig()
		require.Equal(t, clientConfig["DiagnosticId"], id)
	})
}

func TestDatabaseTypeAndMattermostVersion(t *testing.T) {
	// sqlDrivernameEnvironment := os.Getenv("MM_SQLSETTINGS_DRIVERNAME")

	// if sqlDrivernameEnvironment != "" {
	// 	defer os.Setenv("MM_SQLSETTINGS_DRIVERNAME", sqlDrivernameEnvironment)
	// } else {
	// 	defer os.Unsetenv("MM_SQLSETTINGS_DRIVERNAME")
	// }

	t.Setenv("MM_SQLSETTINGS_DRIVERNAME", "postgres")

	th := Setup(t)
	defer th.TearDown()

	databaseType, mattermostVersion, err := th.Service.DatabaseTypeAndSchemaVersion()
	require.NoError(t, err)
	assert.Equal(t, "postgres", databaseType)
	assert.GreaterOrEqual(t, mattermostVersion, strconv.Itoa(1))

	t.Setenv("MM_SQLSETTINGS_DRIVERNAME", "mysql")

	th2 := Setup(t)
	defer th2.TearDown()

	databaseType, mattermostVersion, err = th2.Service.DatabaseTypeAndSchemaVersion()
	require.NoError(t, err)
	assert.Equal(t, "mysql", databaseType)
	assert.GreaterOrEqual(t, mattermostVersion, strconv.Itoa(1))
}
