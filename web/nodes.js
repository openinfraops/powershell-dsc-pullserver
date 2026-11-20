$(document).ready(function() {
    var viewMode = 'active'; // 'active' | 'deleted' (corbeille)
    function agentsEndpoint() {
        return viewMode === 'deleted' ? '/api/v1/agents?deleted=true' : '/api/v1/agents';
    }
    var dataTable = $('#agents-table').DataTable({
        ajax: {
            url: agentsEndpoint(),
            dataSrc: ''
        },
        columns: [
            { data: 'node_name', title: 'Name' },
            { data: 'last_communication', title: 'Last Communication' },
            { data: 'certificate_thumbprint', title: 'Thumbprint' },
            {
                data: 'has_error_last_report',
                title: 'Status',
                className: 'text-center',
                render: function(data, type, row) {
                    if (row.deleted_at) {
                        return `<span class="badge bg-dark" style="min-width:60px;">Corbeille</span>`;
                    }
                    if (row.state) {
                        switch (row.state.toLowerCase()) {
                            case 'success':
                                return `<span class="badge bg-success" style="min-width:60px;">OK</span>`;
                            case 'ok':
                                return `<span class="badge bg-success" style="min-width:60px;">OK</span>`;
                            case 'waiting_for_registration':
                                return `<span class="badge bg-secondary" style="min-width:100px;">Pending Enroll</span>`;
                            case 'failure':
                                return `<span class="badge bg-danger" style="min-width:60px;">Failed</span>`;
                            default:
                                return `<span class="badge bg-info" style="min-width:80px;">${row.state}</span>`;
                        }
                    } else {
                        return `<span class="badge bg-info" style="min-width:80px;">Unknown</span>`;
                    }
                }
            },
            {
                data: 'agent_id',
                title: 'Detail',
                render: function(data, type, row) {
                    const isAdmin = window.currentUser && window.currentUser.role === 'admin';
                    if (row.deleted_at) {
                        // Nœud en corbeille : restaurer (admin) ou consulter.
                        let actions = `<a class='btn btn-outline-secondary btn-sm' href="/web/node/${data}">Voir</a>`;
                        if (isAdmin) {
                            actions = `<button class="btn btn-outline-success btn-sm mr-1 restore-agent-btn" data-id="${data}">Restaurer</button>` + actions;
                        }
                        return actions;
                    }
                    return `<a class='btn btn-info btn-sm' href="/web/node/${data}">View</a>`;
                }
            }
        ]
    });
    $(window).on('resize', function() {
        $('#agents-table').DataTable().columns.adjust();
    });

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
            // header Authorization ajouté globalement par jwt_ajax.js
            success: function(data) {
                $('#preenrollModal').modal('hide');
                $('#preenroll-node-name').val('');
                // Optionally refresh the agents table
                dataTable.ajax.reload(null, false);
            },
            error: function(xhr) {
                $('#preenroll-error').text(xhr.responseText || 'Error');
            }
        });
    });

    // Basculer entre la liste active et la corbeille
    $('#toggle-trash-btn').click(function() {
        viewMode = viewMode === 'deleted' ? 'active' : 'deleted';
        dataTable.ajax.url(agentsEndpoint()).load();
        if (viewMode === 'deleted') {
            $(this).html('<i class="fas fa-server"></i> Voir les nœuds actifs');
            $('#purge-trash-btn').show();
        } else {
            $(this).html('<i class="fas fa-trash"></i> Voir la corbeille');
            $('#purge-trash-btn').hide();
        }
    });

    // Purger la corbeille manuellement (admin)
    $('#purge-trash-btn').click(function() {
        if (!confirm('Purger définitivement tous les nœuds de la corbeille au-delà de la rétention ?')) return;
        $.ajax({
            url: '/api/v1/scheduler/agent-purge/run',
            method: 'POST',
            success: function(res) {
                alert(res && typeof res.purged !== 'undefined' ? ('Purgé ' + res.purged + ' nœud(s).') : 'Corbeille purgée.');
                dataTable.ajax.reload(null, false);
            },
            error: function(xhr) {
                alert('Erreur purge: ' + (xhr.responseText || xhr.status));
            }
        });
    });

    // Restaurer un nœud depuis la corbeille (admin)
    $(document).on('click', '.restore-agent-btn', function() {
        const id = $(this).data('id');
        if (!id) return;
        if (!confirm('Restaurer ce nœud depuis la corbeille ?')) return;
        $.ajax({
            url: '/api/v1/agents/' + encodeURIComponent(id) + '/restore',
            method: 'POST',
            success: function() {
                dataTable.ajax.reload(null, false);
            },
            error: function(xhr) {
                alert('Erreur restauration: ' + (xhr.responseText || xhr.status));
            }
        });
    });
});
