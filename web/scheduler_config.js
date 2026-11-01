$(function() {
    const $status = $('#scheduler-config-status');
    const $tasksBody = $('#scheduler-tasks-body');
    const $historyBody = $('#scheduler-history-body');
    const $historyPrev = $('#scheduler-history-prev');
    const $historyNext = $('#scheduler-history-next');
    const $historyPageInfo = $('#scheduler-history-page-info');
    let schedulerConfig = null;
    let historyTaskName = '';
    let historyOffset = 0;
    const historyLimit = 20;
    let historyHasMore = false;

    const taskRunEndpoints = {
        report_cleanup: '/api/v1/scheduler/cleanup/run',
        release_check: '/api/v1/scheduler/release-check/run',
        log_cleanup: '/api/v1/scheduler/logs/cleanup/run',
        configuration_repo_sync: '/api/v1/scheduler/configuration-repo/run'
    };

    function showStatus(text, color) {
        $status.text(text).css('color', color);
    }

    function normalizeConfig(cfg) {
        const c = cfg || {};
        return {
            enable_report_auto_cleanup: !!c.enable_report_auto_cleanup,
            report_retention_days: c.report_retention_days || 90,
            report_cleanup_interval_mins: c.report_cleanup_interval_mins || 1440,
            enable_release_check: !!c.enable_release_check,
            release_check_interval_mins: c.release_check_interval_mins || 1440,
            enable_log_rotation: c.enable_log_rotation !== false,
            log_rotate_max_size_mb: c.log_rotate_max_size_mb || 10,
            log_rotate_max_backups: c.log_rotate_max_backups || 5,
            log_rotate_max_age_days: c.log_rotate_max_age_days || 30,
            enable_configuration_repo_sync: !!c.enable_configuration_repo_sync,
            configuration_repo_sync_interval_mins: c.configuration_repo_sync_interval_mins || 60,
            configuration_repo_url: c.configuration_repo_url || '',
            configuration_repo_branch: c.configuration_repo_branch || 'main',
            configuration_repo_directory: c.configuration_repo_directory || 'data/configuration-repository',
            configuration_compiler_script: c.configuration_compiler_script || 'scripts/Compile-Configurations.ps1',
            configuration_powershell_executable: c.configuration_powershell_executable || 'pwsh',
            compile_configuration_on_node_registration: !!c.compile_configuration_on_node_registration
        };
    }

    function buildPayload(overrides) {
        const base = normalizeConfig(schedulerConfig);
        return Object.assign(base, overrides || {});
    }

    function saveConfig(payload, onDone) {
        $.ajax({
            url: '/api/v1/scheduler/config',
            method: 'PUT',
            contentType: 'application/json',
            data: JSON.stringify(payload)
        })
            .done(function() {
                schedulerConfig = payload;
                showStatus('Configuration saved.', 'green');
                loadConfig();
                loadTasks();
                if (onDone) {
                    onDone();
                }
            })
            .fail(function(xhr) {
                const msg = (xhr.responseJSON && xhr.responseJSON.error) ? xhr.responseJSON.error : 'Failed to save configuration.';
                showStatus(msg, 'red');
            });
    }

    function openReportConfigModal() {
        const cfg = normalizeConfig(schedulerConfig);
        $('#modal_enable_report_auto_cleanup').prop('checked', cfg.enable_report_auto_cleanup);
        $('#modal_report_retention_days').val(cfg.report_retention_days);
        $('#modal_report_cleanup_interval_mins').val(cfg.report_cleanup_interval_mins);
        $('#reportConfigModal').modal('show');
    }

    function openReleaseConfigModal() {
        const cfg = normalizeConfig(schedulerConfig);
        $('#modal_enable_release_check').prop('checked', cfg.enable_release_check);
        $('#modal_release_check_interval_mins').val(cfg.release_check_interval_mins);
        $('#releaseConfigModal').modal('show');
    }

    function openLogsConfigModal() {
        const cfg = normalizeConfig(schedulerConfig);
        $('#modal_enable_log_rotation').prop('checked', cfg.enable_log_rotation);
        $('#modal_log_rotate_max_size_mb').val(cfg.log_rotate_max_size_mb);
        $('#modal_log_rotate_max_backups').val(cfg.log_rotate_max_backups);
        $('#modal_log_rotate_max_age_days').val(cfg.log_rotate_max_age_days);
        $('#logsConfigModal').modal('show');
    }

    function statusBadge(status) {
        const value = (status || '').toLowerCase();
        if (value === 'success') {
            return '<span class="badge badge-success">Success</span>';
        }
        if (value === 'error') {
            return '<span class="badge badge-danger">Error</span>';
        }
        if (value === 'running') {
            return '<span class="badge badge-info">Running</span>';
        }
        return '<span class="badge badge-secondary">' + (status || 'Idle') + '</span>';
    }

    function isTaskEnabled(taskName) {
        const cfg = normalizeConfig(schedulerConfig);
        if (taskName === 'report_cleanup') {
            return !!cfg.enable_report_auto_cleanup;
        }
        if (taskName === 'release_check') {
            return !!cfg.enable_release_check;
        }
        if (taskName === 'log_cleanup') {
            return !!cfg.enable_log_rotation;
        }
        if (taskName === 'configuration_repo_sync') {
            return !!cfg.enable_configuration_repo_sync;
        }
        return true;
    }

    function enabledBadge(taskName) {
        const enabled = isTaskEnabled(taskName);
        if (enabled) {
            return '<span class="badge badge-success">Enabled</span>';
        }
        return '<span class="badge badge-secondary">Disabled</span>';
    }

    function safe(v) {
        if (v === null || v === undefined || v === '') {
            return '-';
        }
        return String(v)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;');
    }

    function safeAttr(v) {
        if (v === null || v === undefined || v === '') {
            return '-';
        }
        return String(v)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    }

    function formatDateWithTimezone(value) {
        if (!value) {
            return '-';
        }
        const raw = String(value).trim();
        if (!raw) {
            return '-';
        }

        // Backend timestamps are stored/rendered as UTC without offset.
        const utcCandidate = raw.includes('T') ? raw : raw.replace(' ', 'T');
        const dt = new Date(utcCandidate + 'Z');
        if (Number.isNaN(dt.getTime())) {
            return safe(raw);
        }

        return new Intl.DateTimeFormat(undefined, {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
            hour12: false,
            timeZoneName: 'short'
        }).format(dt);
    }

    function loadTasks() {
        $.get('/api/v1/scheduler/tasks')
            .done(function(tasks) {
                if (!Array.isArray(tasks) || tasks.length === 0) {
                    $tasksBody.html('<tr><td colspan="6" class="text-center text-muted">No tasks</td></tr>');
                    return;
                }
                const rows = tasks.map(function(task) {
                    const runBtnDisabled = taskRunEndpoints[task.task_name] ? '' : 'disabled';
                    const note = task.last_message ? '<div class="small text-muted">' + safe(task.last_message) + '</div>' : '';
                    return '<tr>' +
                        '<td>' + safe(task.display_name || task.task_name) + '</td>' +
                        '<td>' + enabledBadge(task.task_name) + '</td>' +
                        '<td>' + formatDateWithTimezone(task.next_run_at) + '</td>' +
                        '<td>' + formatDateWithTimezone(task.last_run_at) + '</td>' +
                        '<td>' + statusBadge(task.last_status) + note + '</td>' +
                        '<td>' +
                            '<button type="button" class="btn btn-sm btn-outline-primary js-task-history" data-task="' + safeAttr(task.task_name) + '">History</button> ' +
                            '<button type="button" class="btn btn-sm btn-outline-secondary js-task-configure" data-task="' + safeAttr(task.task_name) + '">Configure</button> ' +
                            '<button type="button" class="btn btn-sm btn-outline-success js-task-run" data-task="' + safeAttr(task.task_name) + '" ' + runBtnDisabled + '>Run</button>' +
                        '</td>' +
                    '</tr>';
                }).join('');
                $tasksBody.html(rows);
            })
            .fail(function() {
                $tasksBody.html('<tr><td colspan="6" class="text-center text-danger">Failed to load tasks.</td></tr>');
            });
    }

    function updateHistoryPaginationControls() {
        const page = Math.floor(historyOffset / historyLimit) + 1;
        $historyPageInfo.text('Page ' + page + ' (' + historyLimit + ' per page)');
        $historyPrev.prop('disabled', historyOffset <= 0);
        $historyNext.prop('disabled', !historyHasMore);
    }

    function loadHistory(taskName, offset) {
        historyTaskName = taskName;
        historyOffset = Math.max(0, offset || 0);
        $('#schedulerHistoryModalLabel').text('Task history - ' + taskName);
        $historyBody.html('<tr><td colspan="5" class="text-center text-muted">Loading history...</td></tr>');
        historyHasMore = false;
        updateHistoryPaginationControls();
        $('#schedulerHistoryModal').modal('show');

        $.get('/api/v1/scheduler/tasks/' + encodeURIComponent(taskName) + '/history', {
            limit: historyLimit,
            offset: historyOffset
        })
            .done(function(resp) {
                const items = (resp && Array.isArray(resp.items)) ? resp.items : [];
                historyHasMore = !!(resp && resp.has_more);
                updateHistoryPaginationControls();
                if (items.length === 0) {
                    $historyBody.html('<tr><td colspan="5" class="text-center text-muted">No history</td></tr>');
                    return;
                }
                const rows = items.map(function(item) {
                    return '<tr>' +
                        '<td>' + formatDateWithTimezone(item.started_at) + '</td>' +
                        '<td>' + formatDateWithTimezone(item.finished_at) + '</td>' +
                        '<td>' + statusBadge(item.status) + '</td>' +
                        '<td>' + safe(item.trigger_source) + '</td>' +
                        '<td>' + safe(item.message) + '</td>' +
                    '</tr>';
                }).join('');
                $historyBody.html(rows);
            })
            .fail(function() {
                historyHasMore = false;
                updateHistoryPaginationControls();
                $historyBody.html('<tr><td colspan="5" class="text-center text-danger">Failed to load history.</td></tr>');
            });
    }

    function runTask(taskName) {
        const endpoint = taskRunEndpoints[taskName];
        if (!endpoint) {
            showStatus('No run endpoint for task: ' + taskName, 'red');
            return;
        }
        $.ajax({
            url: endpoint,
            method: 'POST'
        })
            .done(function() {
                showStatus('Task launched: ' + taskName, 'green');
                loadTasks();
            })
            .fail(function(xhr) {
                const msg = xhr.responseText || ('Failed to run task: ' + taskName);
                showStatus(msg, 'red');
                loadTasks();
            });
    }

    function loadConfig() {
        $.get('/api/v1/scheduler/config')
            .done(function(cfg) {
                schedulerConfig = normalizeConfig(cfg);
                loadTasks();
            })
            .fail(function() {
                showStatus('Unable to load configuration.', 'red');
            });
    }

    $(document).on('click', '.js-task-history', function() {
        loadHistory($(this).data('task'), 0);
    });

    $historyPrev.on('click', function() {
        if (!historyTaskName || historyOffset <= 0) {
            return;
        }
        loadHistory(historyTaskName, Math.max(0, historyOffset - historyLimit));
    });

    $historyNext.on('click', function() {
        if (!historyTaskName || !historyHasMore) {
            return;
        }
        loadHistory(historyTaskName, historyOffset + historyLimit);
    });

    $(document).on('click', '.js-task-configure', function() {
        const task = $(this).data('task');
        if (task === 'report_cleanup') {
            openReportConfigModal();
            return;
        }
        if (task === 'release_check') {
            openReleaseConfigModal();
            return;
        }
        if (task === 'log_cleanup') {
            openLogsConfigModal();
            return;
        }
        if (task === 'configuration_repo_sync') {
            const cfg = normalizeConfig(schedulerConfig);
            $('#modal_enable_configuration_repo_sync').prop('checked', cfg.enable_configuration_repo_sync);
            $('#modal_configuration_repo_sync_interval_mins').val(cfg.configuration_repo_sync_interval_mins);
            $('#modal_configuration_repo_url').val(cfg.configuration_repo_url);
            $('#modal_configuration_repo_branch').val(cfg.configuration_repo_branch);
            $('#modal_configuration_repo_directory').val(cfg.configuration_repo_directory);
            $('#modal_configuration_compiler_script').val(cfg.configuration_compiler_script);
            $('#modal_configuration_powershell_executable').val(cfg.configuration_powershell_executable);
            $('#modal_compile_configuration_on_node_registration').prop('checked', cfg.compile_configuration_on_node_registration);
            $('#configurationRepoSyncModal').modal('show');
            return;
        }
    });

    $('#save-report-config').on('click', function() {
        const payload = buildPayload({
            enable_report_auto_cleanup: $('#modal_enable_report_auto_cleanup').prop('checked'),
            report_retention_days: parseInt($('#modal_report_retention_days').val(), 10),
            report_cleanup_interval_mins: parseInt($('#modal_report_cleanup_interval_mins').val(), 10)
        });
        saveConfig(payload, function() {
            $('#reportConfigModal').modal('hide');
        });
    });

    $('#save-release-config').on('click', function() {
        const payload = buildPayload({
            enable_release_check: $('#modal_enable_release_check').prop('checked'),
            release_check_interval_mins: parseInt($('#modal_release_check_interval_mins').val(), 10)
        });
        saveConfig(payload, function() {
            $('#releaseConfigModal').modal('hide');
        });
    });

    $('#save-logs-config').on('click', function() {
        const payload = buildPayload({
            enable_log_rotation: $('#modal_enable_log_rotation').prop('checked'),
            log_rotate_max_size_mb: parseInt($('#modal_log_rotate_max_size_mb').val(), 10),
            log_rotate_max_backups: parseInt($('#modal_log_rotate_max_backups').val(), 10),
            log_rotate_max_age_days: parseInt($('#modal_log_rotate_max_age_days').val(), 10)
        });
        saveConfig(payload, function() {
            $('#logsConfigModal').modal('hide');
        });
    });

    $('#save-configuration-repo-config').on('click', function() {
        const payload = buildPayload({
            enable_configuration_repo_sync: $('#modal_enable_configuration_repo_sync').prop('checked'),
            configuration_repo_sync_interval_mins: parseInt($('#modal_configuration_repo_sync_interval_mins').val(), 10),
            configuration_repo_url: $('#modal_configuration_repo_url').val().trim(),
            configuration_repo_branch: $('#modal_configuration_repo_branch').val().trim(),
            configuration_repo_directory: $('#modal_configuration_repo_directory').val().trim(),
            configuration_compiler_script: $('#modal_configuration_compiler_script').val().trim(),
            configuration_powershell_executable: $('#modal_configuration_powershell_executable').val().trim(),
            compile_configuration_on_node_registration: $('#modal_compile_configuration_on_node_registration').prop('checked')
        });
        saveConfig(payload, function() {
            $('#configurationRepoSyncModal').modal('hide');
        });
    });

    $(document).on('click', '.js-task-run', function() {
        runTask($(this).data('task'));
    });

    loadConfig();
    loadTasks();
});
