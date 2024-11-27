// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package app

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/mattermost/mattermost/server/public/model"
	smocks "github.com/mattermost/mattermost/server/v8/channels/store/storetest/mocks"
	"github.com/mattermost/mattermost/server/v8/config"
)

func TestGenerateSupportPacket(t *testing.T) {
	th := Setup(t)
	defer th.TearDown()

	dir, err := os.MkdirTemp("", "")
	require.NoError(t, err)
	t.Cleanup(func() {
		err = os.RemoveAll(dir)
		assert.NoError(t, err)
	})

	th.App.UpdateConfig(func(cfg *model.Config) {
		*cfg.LogSettings.FileLocation = dir
		*cfg.NotificationLogSettings.FileLocation = dir
	})

	logLocation := config.GetLogFileLocation(dir)
	notificationsLogLocation := config.GetNotificationsLogFileLocation(dir)

	genMockLogFiles := func() {
		d1 := []byte("hello\ngo\n")
		genErr := os.WriteFile(logLocation, d1, 0777)
		require.NoError(t, genErr)
		genErr = os.WriteFile(notificationsLogLocation, d1, 0777)
		require.NoError(t, genErr)
	}
	genMockLogFiles()

	t.Run("generate Support Packet with logs", func(t *testing.T) {
		fileDatas := th.App.GenerateSupportPacket(th.Context, &model.SupportPacketOptions{
			IncludeLogs: true,
		})
		var rFileNames []string
		testFiles := []string{
			"metadata.yaml",
			"stats.yaml",
			"jobs.yaml",
			"plugins.json",
			"sanitized_config.json",
			"diagnostics.yaml",
			"mattermost.log",
			"notifications.log",
			"cpu.prof",
			"heap.prof",
			"goroutines",
		}
		for _, fileData := range fileDatas {
			require.NotNil(t, fileData)
			assert.Positive(t, len(fileData.Body))

			rFileNames = append(rFileNames, fileData.Filename)
		}
		assert.ElementsMatch(t, testFiles, rFileNames)
	})

	t.Run("generate Support Packet without logs", func(t *testing.T) {
		fileDatas := th.App.GenerateSupportPacket(th.Context, &model.SupportPacketOptions{
			IncludeLogs: false,
		})

		testFiles := []string{
			"metadata.yaml",
			"stats.yaml",
			"jobs.yaml",
			"plugins.json",
			"sanitized_config.json",
			"diagnostics.yaml",
			"cpu.prof",
			"heap.prof",
			"goroutines",
		}
		var rFileNames []string
		for _, fileData := range fileDatas {
			require.NotNil(t, fileData)
			assert.Positive(t, len(fileData.Body))

			rFileNames = append(rFileNames, fileData.Filename)
		}
		assert.ElementsMatch(t, testFiles, rFileNames)
	})

	t.Run("remove the log files and ensure that warning.txt file is generated", func(t *testing.T) {
		// Remove these two files and ensure that warning.txt file is generated
		err = os.Remove(logLocation)
		require.NoError(t, err)
		err = os.Remove(notificationsLogLocation)
		require.NoError(t, err)
		t.Cleanup(genMockLogFiles)

		fileDatas := th.App.GenerateSupportPacket(th.Context, &model.SupportPacketOptions{
			IncludeLogs: true,
		})
		testFiles := []string{
			"metadata.yaml",
			"stats.yaml",
			"jobs.yaml",
			"plugins.json",
			"sanitized_config.json",
			"diagnostics.yaml",
			"cpu.prof",
			"heap.prof",
			"warning.txt",
			"goroutines",
		}
		var rFileNames []string
		for _, fileData := range fileDatas {
			require.NotNil(t, fileData)
			assert.Positive(t, len(fileData.Body))

			rFileNames = append(rFileNames, fileData.Filename)
		}
		assert.ElementsMatch(t, testFiles, rFileNames)
	})

	t.Run("steps that generated an error should still return file data", func(t *testing.T) {
		mockStore := smocks.Store{}

		// Mock the post store to trigger an error
		ps := &smocks.PostStore{}
		ps.On("AnalyticsPostCount", &model.PostCountOptions{}).Return(int64(0), errors.New("all broken"))
		ps.On("ClearCaches")
		mockStore.On("Post").Return(ps)

		mockStore.On("User").Return(th.App.Srv().Store().User())
		mockStore.On("Channel").Return(th.App.Srv().Store().Channel())
		mockStore.On("Post").Return(th.App.Srv().Store().Post())
		mockStore.On("Team").Return(th.App.Srv().Store().Team())
		mockStore.On("Job").Return(th.App.Srv().Store().Job())
		mockStore.On("FileInfo").Return(th.App.Srv().Store().FileInfo())
		mockStore.On("Webhook").Return(th.App.Srv().Store().Webhook())
		mockStore.On("System").Return(th.App.Srv().Store().System())
		mockStore.On("License").Return(th.App.Srv().Store().License())
		mockStore.On("Command").Return(th.App.Srv().Store().Command())
		mockStore.On("Close").Return(nil)
		mockStore.On("GetDBSchemaVersion").Return(1, nil)
		mockStore.On("GetDbVersion", false).Return("1.0.0", nil)
		mockStore.On("TotalMasterDbConnections").Return(30)
		mockStore.On("TotalReadDbConnections").Return(20)
		mockStore.On("TotalSearchDbConnections").Return(10)
		th.App.Srv().SetStore(&mockStore)

		fileDatas := th.App.GenerateSupportPacket(th.Context, &model.SupportPacketOptions{
			IncludeLogs: false,
		})

		var rFileNames []string
		for _, fileData := range fileDatas {
			require.NotNil(t, fileData)
			assert.Positive(t, len(fileData.Body))

			rFileNames = append(rFileNames, fileData.Filename)
		}
		assert.Contains(t, rFileNames, "warning.txt")
		assert.Contains(t, rFileNames, "stats.yaml")
	})
}

func TestGetPluginsFile(t *testing.T) {
	th := Setup(t)
	defer th.TearDown()

	// Happy path where we have a plugins file with no err
	fileData, err := th.App.getPluginsFile(th.Context)
	require.NotNil(t, fileData)
	assert.Equal(t, "plugins.json", fileData.Filename)
	assert.Positive(t, len(fileData.Body))
	assert.NoError(t, err)

	// Turn off plugins so we can get an error
	th.App.UpdateConfig(func(cfg *model.Config) {
		*cfg.PluginSettings.Enable = false
	})

	// Plugins off in settings so no fileData and we get a warning instead
	fileData, err = th.App.getPluginsFile(th.Context)
	assert.Nil(t, fileData)
	assert.ErrorContains(t, err, "failed to get plugin list for Support Packet")
}

func TestGetSupportPacketStats(t *testing.T) {
	th := Setup(t).InitBasic()
	defer th.TearDown()

	t.Setenv(envVarInstallType, "docker")

	licenseUsers := 100
	license := model.NewTestLicense("ldap")
	license.Features.Users = model.NewPointer(licenseUsers)
	th.App.Srv().SetLicense(license)

	generateSupportPacket := func(t *testing.T) *model.SupportPacketStats {
		t.Helper()

		fileData, err := th.App.getSupportPacketStats(th.Context)
		require.NotNil(t, fileData)
		assert.Equal(t, "stats.yaml", fileData.Filename)
		assert.Positive(t, len(fileData.Body))
		assert.NoError(t, err)

		var packet model.SupportPacketStats
		require.NoError(t, yaml.Unmarshal(fileData.Body, &packet))
		return &packet
	}

	t.Run("Happy path", func(t *testing.T) {
		sp := generateSupportPacket(t)

		assert.Equal(t, int64(3), sp.RegisteredUsers) // from InitBasic()
		assert.Equal(t, int64(3), sp.ActiveUsers)     // from InitBasic()
		assert.Equal(t, int64(0), sp.DailyActiveUsers)
		assert.Equal(t, int64(0), sp.MonthlyActiveUsers)
		assert.Equal(t, int64(0), sp.DeactivatedUsers)
		assert.Equal(t, int64(0), sp.Guests)
		assert.Equal(t, int64(0), sp.BotAccounts)
		assert.Equal(t, int64(5), sp.Posts)    // from InitBasic()
		assert.Equal(t, int64(3), sp.Channels) // from InitBasic()
		assert.Equal(t, int64(1), sp.Teams)    // from InitBasic()
		assert.Equal(t, int64(0), sp.SlashCommands)
		assert.Equal(t, int64(0), sp.IncomingWebhooks)
		assert.Equal(t, int64(0), sp.OutgoingWebhooks)
	})

	t.Run("post count should be present if number of users extends AnalyticsSettings.MaxUsersForStatistics", func(t *testing.T) {
		th.App.UpdateConfig(func(cfg *model.Config) {
			cfg.AnalyticsSettings.MaxUsersForStatistics = model.NewPointer(1)
		})

		for i := 0; i < 5; i++ {
			p := th.CreatePost(th.BasicChannel)
			require.NotNil(t, p)
		}

		// InitBasic() already creats 5 posts
		packet := generateSupportPacket(t)
		assert.Equal(t, int64(10), packet.Posts)
	})
}

func TestGetSupportPacketJobList(t *testing.T) {
	th := Setup(t)
	defer th.TearDown()

	getJobList := func(t *testing.T) *model.SupportPacketJobList {
		t.Helper()

		fileData, err := th.App.getSupportPacketJobList(th.Context)
		require.NoError(t, err)
		require.NotNil(t, fileData)
		assert.Equal(t, "jobs.yaml", fileData.Filename)
		assert.Positive(t, len(fileData.Body))

		var jobs model.SupportPacketJobList
		require.NoError(t, yaml.Unmarshal(fileData.Body, &jobs))
		return &jobs
	}

	t.Run("no jobs run yet", func(t *testing.T) {
		jobs := getJobList(t)

		assert.Empty(t, jobs.LDAPSyncJobs)
		assert.Empty(t, jobs.DataRetentionJobs)
		assert.Empty(t, jobs.MessageExportJobs)
		assert.Empty(t, jobs.ElasticPostIndexingJobs)
		assert.Empty(t, jobs.ElasticPostAggregationJobs)
		assert.Empty(t, jobs.BlevePostIndexingJobs)
		assert.Empty(t, jobs.MigrationJobs)
	})
}

func TestGetSanitizedConfigFile(t *testing.T) {
	th := Setup(t)
	defer th.TearDown()

	// Happy path where we have a sanitized config file with no err
	fileData, err := th.App.getSanitizedConfigFile(th.Context)
	require.NotNil(t, fileData)
	assert.Equal(t, "sanitized_config.json", fileData.Filename)
	assert.Positive(t, len(fileData.Body))
	assert.NoError(t, err)
}

func TestGetSupportPacketMetadata(t *testing.T) {
	th := Setup(t)
	defer th.TearDown()

	t.Run("Happy path", func(t *testing.T) {
		fileData, err := th.App.getSupportPacketMetadata(th.Context)
		require.NoError(t, err)
		require.NotNil(t, fileData)
		assert.Equal(t, "metadata.yaml", fileData.Filename)
		assert.Positive(t, len(fileData.Body))

		metadate, err := model.ParsePacketMetadata(fileData.Body)
		assert.NoError(t, err)
		require.NotNil(t, metadate)
		assert.Equal(t, model.SupportPacketType, metadate.Type)
		assert.Equal(t, model.CurrentVersion, metadate.ServerVersion)
		assert.NotEmpty(t, metadate.ServerID)
		assert.NotEmpty(t, metadate.GeneratedAt)
	})
}
