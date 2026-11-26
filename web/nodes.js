$(document).ready(function() {
    var viewMode = 'active'; // 'active' | 'deleted' (corbeille)
    var staleFilter = false; // filtre "nœuds muets"
    var certFilter = false;  // filtre "certificats à risque"
    var tagFilter = { key: '', value: '' };
    var selected = {}; // agent_id -> true, persisté entre les pages
    var STALE_DAYS = 7;
    var CERT_EXPIRING_DAYS = 30;

    // --- Helpers -------------------------------------------------------------

    function esc(text) {
        if (text === null || text === undefined) return '';
        return String(text)
            .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
    }

    // Parse une date stockée "YYYY-MM-DD HH:MM:SS" (UTC) en Date JS.
    function parseUtc(s) {
        if (!s) return null;
        var str = String(s).trim();
        // "YYYY-MM-DD[ T]HH:MM:SS[.f...][Z|z]" -> traité comme UTC. DSC envoie
        // parfois une fraction à 7 chiffres (.0000000Z) que Date() rejette ;
        // on la tronque à 3 chiffres (millisecondes) pour un parsing fiable.
        var m = str.match(/^(\d{4}-\d{2}-\d{2})[\sT]+(\d{2}:\d{2}:\d{2})(?:\.(\d+))?(?:Z|z)?$/);
        if (m) {
            var frac = (m[3] || '').slice(0, 3);
            var iso = m[1] + 'T' + m[2] + (frac ? '.' + frac : '') + 'Z';
            var d = new Date(iso);
            return isNaN(d.getTime()) ? null : d;
        }
        // Format avec fuseau explicite (±HH:MM) ou autre ISO : parsing natif.
        var d2 = new Date(str);
        return isNaN(d2.getTime()) ? null : d2;
    }

    function isStale(lastComm) {
        var d = parseUtc(lastComm);
        if (!d) return false;
        var cutoff = new Date(Date.now() - STALE_DAYS * 24 * 60 * 60 * 1000);
        return d < cutoff;
    }

    function formatDate(dateStr) {
        if (!dateStr) return '';
        var d = parseUtc(dateStr);
        if (!d) return dateStr;
        return d.toLocaleString();
    }

    // Jours restants avant expiration du certificat (négatif si déjà expiré).
    function daysUntil(dateStr) {
        var d = parseUtc(dateStr);
        if (!d) return null;
        return Math.ceil((d.getTime() - Date.now()) / (1000 * 60 * 60 * 24));
    }

    // Badge de statut du certificat : expiré / expire bientôt / valide / absent.
    function certRender(data, type, row) {
        if (type === 'sort') return data || '';
        var notAfter = row.certificate_notafter || '';
        if (!notAfter) {
            return '<span class="badge bg-secondary text-light" title="No certificate">N/A</span>';
        }
        var days = daysUntil(notAfter);
        if (days === null || isNaN(days)) {
            return '<span class="badge bg-secondary text-light" title="Unparseable date">' + esc(notAfter) + '</span>';
        }
        if (days < 0) {
            return '<span class="badge bg-danger" title="Expired on ' + esc(notAfter) + '">Expired</span>';
        }
        if (days <= CERT_EXPIRING_DAYS) {
            return '<span class="badge bg-warning text-dark" title="Expires on ' + esc(notAfter) + '">' + days + ' d</span>';
        }
        return '<span class="badge bg-success" title="Expires on ' + esc(notAfter) + '">' + days + ' d</span>';
    }

    function agentsEndpoint() {
        return '/api/v1/agents';
    }

    function selectedIds() {
        return Object.keys(selected);
    }

    function updateBulkToolbar() {
        var count = selectedIds().length;
        $('#bulk-selection-count').text(count);
        if (count > 0) {
            $('#bulk-toolbar').show();
            $('#bulk-restore-btn').toggle(viewMode === 'deleted');
            // Les actions de mise en corbeille/recompiler/tag ne s'appliquent qu'aux nœuds actifs.
            var active = viewMode !== 'deleted';
            var isAdmin = window.currentUser && window.currentUser.role === 'admin';
            $('#bulk-recompile-btn').toggle(active);
            $('#bulk-tag-btn').toggle(active && isAdmin);
            $('#bulk-trash-btn').toggle(active);
        } else {
            $('#bulk-toolbar').hide();
        }
    }

    function clearSelection() {
        selected = {};
        $('.agent-select-checkbox').prop('checked', false);
        $('#select-all-rows').prop('checked', false);
        updateBulkToolbar();
    }

    function afterBulk(res) {
        var msg = 'Processed: ' + res.processed + ' / Failed: ' + res.failed;
        if(res.errors && res.errors.length) msg += '\n' + res.errors.join('\n');
        alert(msg);
        clearSelection();
        dataTable.ajax.reload(null, false);
    }

    function bulkPost(url) {
        var ids = selectedIds();
        if(!ids.length) return;
        $.ajax({
            url: url, method: 'POST', contentType: 'application/json',
            data: JSON.stringify({ agent_ids: ids }),
            success: function(res) { afterBulk(res); },
            error: function(xhr) { alert('Error: ' + (xhr.responseText || xhr.status)); }
        });
    }

    function bulkPut(url, extra) {
        var ids = selectedIds();
        if(!ids.length) return;
        var payload = { agent_ids: ids };
        $.extend(payload, extra);
        $.ajax({
            url: url, method: 'PUT', contentType: 'application/json',
            data: JSON.stringify(payload),
            success: function(res) { afterBulk(res); },
            error: function(xhr) { alert('Error: ' + (xhr.responseText || xhr.status)); }
        });
    }

    // --- Tableau (server-side) ----------------------------------------------

    function statusRender(data, type, row) {
        if (row.deleted_at) {
            return '<span class="badge bg-dark" style="min-width:60px;">Trash</span>';
        }
        var state = (row.state || '').toLowerCase();
        var badge;
        if (state === 'success' || state === 'ok') {
            badge = '<span class="badge bg-success" style="min-width:60px;">OK</span>';
        } else if (state === 'waiting_for_registration') {
            badge = '<span class="badge bg-secondary" style="min-width:100px;">Pending Enroll</span>';
        } else if (state === 'failure') {
            badge = '<span class="badge bg-danger" style="min-width:60px;">Failed</span>';
        } else if (state) {
            badge = '<span class="badge bg-info" style="min-width:80px;">' + esc(row.state) + '</span>';
        } else {
            badge = '<span class="badge bg-info" style="min-width:80px;">Unknown</span>';
        }
        if (isStale(row.last_communication)) {
            badge += ' <span class="badge bg-warning text-dark" style="min-width:60px;">Silent</span>';
        }
        return badge;
    }

    function detailRender(data, type, row) {
        const isAdmin = window.currentUser && window.currentUser.role === 'admin';
        if (row.deleted_at) {
            let actions = `<a class='btn btn-outline-secondary btn-sm' href="/web/node/${encodeURIComponent(data)}">View</a>`;
            if (isAdmin) {
                actions = `<button class="btn btn-outline-success btn-sm mr-1 restore-agent-btn" data-id="${encodeURIComponent(data)}">Restore</button>` + actions;
            }
            return actions;
        }
        return `<a class='btn btn-info btn-sm' href="/web/node/${encodeURIComponent(data)}">View</a>`;
    }

    // Synchronise l'URL avec l'état du filtre "certificats à risque" (lien
    // dashboard -> ?cert_at_risk=1). Le filtre devient déterministe : activé, il
    // survit à un rechargement ; désactivé, le param est retiré pour que l'état
    // ne soit pas "réimposé" par une URL qui porte encore cert_at_risk=1.
    function syncCertFilterUrl(enabled) {
        try {
            var url = new URL(window.location.href);
            if (enabled) {
                url.searchParams.set('cert_at_risk', '1');
            } else {
                url.searchParams.delete('cert_at_risk');
            }
            history.replaceState(null, '', url.toString());
        } catch (e) { /* URL malformée : on ignore la synchro. */ }
    }

    // Active automatiquement le filtre "certificats à risque" si l'URL contient
    // ?cert_at_risk=1 (lien depuis le dashboard). Le DataTable doit être
    // initialisé APRÈS ce réglage : avec serverSide, sa première requête ajax est
    // émise pendant l'initialisation, donc certFilter doit déjà être à true pour
    // que cette requête porte le filtre (sinon la liste s'affiche non filtrée et
    // le reste faute de reload).
    if (/[?&]cert_at_risk=1\b/.test(window.location.search)) {
        certFilter = true;
        $('#cert-filter-indicator').show();
        $('#cert-filter-btn').addClass('active');
    }

    var dataTable = $('#agents-table').DataTable({
        processing: true,
        serverSide: true,
        ajax: function(data, callback) {
            var params = {
                draw: data.draw,
                start: data.start,
                length: data.length,
                'search[value]': (data.search && data.search.value) || '',
                'order[0][column]': (data.order && data.order.length) ? data.order[0].column : 1,
                'order[0][dir]': (data.order && data.order.length) ? data.order[0].dir : 'asc'
            };
            if (viewMode === 'deleted') params.deleted = 'true';
            if (staleFilter) params.stale = 'true';
            if (certFilter) params['cert_at_risk'] = 'true';
            if (tagFilter.key && tagFilter.value) params['tag'] = tagFilter.key + ':' + tagFilter.value;
            $.ajax({
                url: agentsEndpoint(),
                type: 'GET',
                data: params,
                dataType: 'json',
                success: function(resp) { callback(resp); },
                error: function(xhr) { callback({ draw: data.draw, recordsTotal: 0, recordsFiltered: 0, data: [] }); }
            });
        },
        columns: [
            {
                data: null, orderable: false, searchable: false, width: '30px',
                render: function(data, type, row) {
                    var checked = selected[row.agent_id] ? ' checked' : '';
                    return '<input type="checkbox" class="agent-select-checkbox" value="' + esc(row.agent_id) + '"' + checked + '>';
                }
            },
            { data: 'node_name', title: 'Name' },
            {
                data: 'last_communication', title: 'Last Communication',
                render: function(data, type) { return type === 'sort' ? data : formatDate(data); }
            },
            {
                data: 'certificate_thumbprint', title: 'Thumbprint',
                render: function(data) { return data ? esc(data) : ''; }
            },
            { data: 'has_error_last_report', title: 'Status', className: 'text-center', render: statusRender },
            {
                data: 'certificate_notafter', title: 'Certificates', className: 'text-center',
                render: certRender
            },
            { data: 'agent_id', title: 'Detail', render: detailRender }
        ],
        order: [[1, 'asc']],
        pageLength: 25,
        lengthMenu: [10, 25, 50, 100]
    });

    $(window).on('resize', function() {
        $('#agents-table').DataTable().columns.adjust();
    });

    // --- Sélection / actions en masse ---------------------------------------

    $(document).on('change', '.agent-select-checkbox', function() {
        var id = $(this).val();
        if (this.checked) selected[id] = true; else delete selected[id];
        updateBulkToolbar();
    });

    $('#select-all-rows').on('change', function() {
        var checked = this.checked;
        $('.agent-select-checkbox').each(function() {
            var id = $(this).val();
            if (checked) selected[id] = true; else delete selected[id];
        });
        $('.agent-select-checkbox').prop('checked', checked);
        updateBulkToolbar();
    });

    $('#bulk-recompile-btn').click(function() {
        bulkPost('/api/v1/agents/bulk/recompile');
    });
    $('#bulk-trash-btn').click(function() {
        if (!confirm('Move the selected nodes to trash?')) return;
        bulkPost('/api/v1/agents/bulk/trash');
    });
    $('#bulk-restore-btn').click(function() {
        if (!confirm('Restore the selected nodes from trash?')) return;
        bulkPost('/api/v1/agents/bulk/restore');
    });
    $('#bulk-tag-btn').click(function() {
        var key = prompt('Tag key (e.g. environment)');
        if (!key) return;
        var value = prompt('Tag value (e.g. production)');
        if (!value) return;
        bulkPut('/api/v1/agents/bulk/tags', { key: key, value: value });
    });
    $('#bulk-clear-btn').click(function() {
        clearSelection();
    });

    // --- Filtres -------------------------------------------------------------

    // Filtre "nœuds muets"
    $('#stale-filter-btn').click(function() {
        staleFilter = !staleFilter;
        $('#stale-filter-indicator').toggle(staleFilter);
        $(this).toggleClass('active', staleFilter);
        dataTable.ajax.reload(null, false);
    });

    // Filtre "certificats à risque" (expiré ou expirant sous N jours)
    $('#cert-filter-btn').click(function() {
        certFilter = !certFilter;
        $('#cert-filter-indicator').toggle(certFilter);
        $(this).toggleClass('active', certFilter);
        syncCertFilterUrl(certFilter);
        dataTable.ajax.reload(null, false);
    });

    // Options de clés de tag pour le filtre
    $.getJSON('/api/v1/agents/tags/options', function(resp) {
        var keys = (resp && resp.keys) || [];
        var html = '<option value="">(all)</option>';
        keys.forEach(function(k) { html += '<option value="' + esc(k) + '">' + esc(k) + '</option>'; });
        $('#tag-filter-key').html(html);
    });

    $('#tag-filter-apply').click(function() {
        tagFilter.key = $('#tag-filter-key').val();
        tagFilter.value = $('#tag-filter-value').val().trim();
        if (!tagFilter.key || !tagFilter.value) {
            alert('Choose a tag key and enter a value.');
            return;
        }
        dataTable.ajax.reload(null, false);
    });
    $('#tag-filter-clear').click(function() {
        tagFilter = { key: '', value: '' };
        $('#tag-filter-key').val('');
        $('#tag-filter-value').val('');
        dataTable.ajax.reload(null, false);
    });

    // --- Pré-enregistrement --------------------------------------------------

    $('#preenroll-btn').click(function() {
        $('#preenrollModal').modal('show');
    });
    $('#preenroll-submit').click(function() {
        var nodeName = $('#preenroll-node-name').val();
        if (!nodeName) {
            $('#preenroll-error').text('Node name is required');
            return;
        }
        $('#preenroll-error').text('');
        $.ajax({
            url: '/api/v1/agents/preenroll',
            method: 'POST',
            contentType: 'application/json',
            data: JSON.stringify({ node_name: nodeName }),
            success: function(data) {
                $('#preenrollModal').modal('hide');
                $('#preenroll-node-name').val('');
                dataTable.ajax.reload(null, false);
            },
            error: function(xhr) {
                $('#preenroll-error').text(xhr.responseText || 'Error');
            }
        });
    });

    // --- Corbeille -----------------------------------------------------------

    $('#toggle-trash-btn').click(function() {
        viewMode = viewMode === 'deleted' ? 'active' : 'deleted';
        clearSelection();
        dataTable.ajax.reload(null, false);
        if (viewMode === 'deleted') {
            $(this).html('<i class="fas fa-server"></i> View active nodes');
            $('#purge-trash-btn').show();
        } else {
            $(this).html('<i class="fas fa-trash"></i> View trash');
            $('#purge-trash-btn').hide();
        }
    });

    $('#purge-trash-btn').click(function() {
        if (!confirm('Permanently purge all trashed nodes beyond the retention period?')) return;
        $.ajax({
            url: '/api/v1/scheduler/agent-purge/run',
            method: 'POST',
            success: function(res) {
                alert(res && typeof res.purged !== 'undefined' ? ('Purged ' + res.purged + ' node(s).') : 'Trash purged.');
                dataTable.ajax.reload(null, false);
            },
            error: function(xhr) {
                alert('Purge error: ' + (xhr.responseText || xhr.status));
            }
        });
    });

    // Restaurer un nœud individuel depuis la corbeille (admin)
    $(document).on('click', '.restore-agent-btn', function() {
        const id = $(this).data('id');
        if (!id) return;
        if (!confirm('Restore this node from trash?')) return;
        $.ajax({
            url: '/api/v1/agents/' + encodeURIComponent(id) + '/restore',
            method: 'POST',
            success: function() {
                dataTable.ajax.reload(null, false);
            },
            error: function(xhr) {
                alert('Restore error: ' + (xhr.responseText || xhr.status));
            }
        });
    });

    // --- Export CSV ----------------------------------------------------------

    $('#export-agents-csv-btn').click(function() {
        var url = '/api/v1/agents/export?deleted=' + (viewMode === 'deleted' ? 'true' : 'false');
        if (staleFilter) url += '&stale=true';
        if (certFilter) url += '&cert_at_risk=true';
        if (tagFilter.key && tagFilter.value) url += '&tag=' + encodeURIComponent(tagFilter.key + ':' + tagFilter.value);
        var xhr = new XMLHttpRequest();
        xhr.open('GET', url, true);
        xhr.responseType = 'blob';
        xhr.onload = function() {
            if(xhr.status === 200) {
                var blob = xhr.response;
                var link = document.createElement('a');
                link.href = window.URL.createObjectURL(blob);
                link.download = viewMode === 'deleted' ? 'agents_trash.csv' : 'agents_export.csv';
                document.body.appendChild(link);
                link.click();
                document.body.removeChild(link);
            } else {
                alert('Export failed: ' + xhr.status);
            }
        };
        xhr.onerror = function() { alert('Export error'); };
        xhr.send();
    });
});
