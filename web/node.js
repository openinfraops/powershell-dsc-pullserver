function renderNodeActionBlock() {
    const nodeId = getNodeIdFromUrl();
    if (!nodeId || !state.agent) return '';
    const isAdmin = window.currentUser && window.currentUser.role === 'admin';
    const isDeleted = !!state.agent.deleted_at;
    let html = '';
    if (isDeleted) {
        // Nœud en corbeille : on ne peut que le restaurer (admin).
        if (isAdmin) {
            html += '<button id="restore-node-btn" class="btn btn-success btn-sm">Restaurer ce nœud</button>';
        }
        return html;
    }
    // Recalcul de la configuration : admin uniquement, visible quand la config est obsolète.
    if (isAdmin && state.agent.needs_recompile) {
        html += '<button id="recompile-node-btn" class="btn btn-warning btn-sm mr-1">Recalculer la configuration</button>';
    }
    // Suppression : admin uniquement.
    if (isAdmin) {
        html += '<button id="delete-node-btn" class="btn btn-danger btn-sm">Supprimer ce nœud</button>';
    }
    return html;
}

function showNodeMessage(msg, type) {
    // type: 'success' | 'error' | 'info'
    let color = type === 'success' ? '#d4edda' : (type === 'error' ? '#f8d7da' : '#e2e3e5');
    let txtColor = type === 'success' ? '#155724' : (type === 'error' ? '#721c24' : '#383d41');
    $('#node-message-block').html('<div style="background:'+color+';color:'+txtColor+';padding:8px 12px;border-radius:4px;margin-bottom:8px;">'+msg+'</div>');
}

// Suppression : ouvre une modale demandant de retaper le nom d'hôte.
$(document).on('click', '#delete-node-btn', function() {
    const nodeId = getNodeIdFromUrl();
    if (!nodeId || !state.agent) return;
    const requiredName = (state.agent.node_name || '').trim();
    $('#delete-required-name').text(requiredName);
    $('#delete-confirm-input').val('');
    $('#delete-node-confirm-btn').prop('disabled', true);
    $('#delete-node-modal').modal('show');
});

// Active le bouton "Supprimer définitivement" uniquement si le nom d'hôte a été retapé
// (comparaison insensible à la casse et aux espaces).
$(document).on('input', '#delete-confirm-input', function() {
    const requiredName = (state.agent && state.agent.node_name || '').trim();
    const typed = $(this).val().trim();
    if (requiredName && typed.toLowerCase() === requiredName.toLowerCase()) {
        $('#delete-node-confirm-btn').prop('disabled', false);
    } else {
        $('#delete-node-confirm-btn').prop('disabled', true);
    }
});

$(document).on('click', '#delete-node-confirm-btn', function() {
    const nodeId = getNodeIdFromUrl();
    if (!nodeId) return;
    const requiredName = (state.agent && state.agent.node_name || '').trim();
    const typed = $('#delete-confirm-input').val().trim();
    if (!requiredName || typed.toLowerCase() !== requiredName.toLowerCase()) {
        $('#delete-node-confirm-btn').prop('disabled', true);
        return;
    }
    $.ajax({
        url: '/api/v1/agents/' + encodeURIComponent(nodeId),
        type: 'DELETE',
        success: function() {
            $('#delete-node-modal').modal('hide');
            showNodeMessage('Noeud déplacé vers la corbeille.', 'success');
            setTimeout(function(){ window.location.href = '/web'; }, 1200);
        },
        error: function(xhr) {
            $('#delete-node-modal').modal('hide');
            showNodeMessage('Erreur lors de la suppression du noeud: ' + (xhr.responseText || xhr.status), 'error');
        }
    });
});

// Restauration d'un nœud depuis la corbeille.
$(document).on('click', '#restore-node-btn', function() {
    const nodeId = getNodeIdFromUrl();
    if (!nodeId) return;
    if (!confirm('Restaurer ce nœud depuis la corbeille ?')) return;
    $.ajax({
        url: '/api/v1/agents/' + encodeURIComponent(nodeId) + '/restore',
        type: 'POST',
        success: function() {
            showNodeMessage('Nœud restauré.', 'success');
            setTimeout(function(){ window.location.reload(); }, 1000);
        },
        error: function(xhr) {
            showNodeMessage('Erreur lors de la restauration: ' + (xhr.responseText || xhr.status), 'error');
        }
    });
});

$(document).on('click', '#recompile-node-btn', function() {
    const nodeId = getNodeIdFromUrl();
    if (!nodeId) return;
    if (!confirm('Recalculer la configuration de ce nœud à partir des tags actuels ?')) return;
    $.ajax({
        url: '/api/v1/agents/' + encodeURIComponent(nodeId) + '/recompile',
        type: 'POST',
        success: function() {
            showNodeMessage('Recalcul de la configuration lancé.', 'success');
            let tries = 0;
            const poll = setInterval(function() {
                tries++;
                $.ajax({
                    url: '/api/v1/agents/' + encodeURIComponent(nodeId),
                    method: 'GET',
                    dataType: 'json',
                    success: function(agent) {
                        state.agent = agent;
                        renderAll();
                        if (!state.agent.needs_recompile || tries > 30) {
                            clearInterval(poll);
                            showNodeMessage(state.agent.needs_recompile ? 'Recalcul toujours en attente…' : 'Configuration recalculée.', state.agent.needs_recompile ? 'info' : 'success');
                        }
                    }
                });
            }, 5000);
        },
        error: function(xhr) {
            showNodeMessage('Erreur lors du recalcul: ' + (xhr.responseText || xhr.status), 'error');
        }
    });
});
// node.js (frontend) pour page /web/node/{id}

function getNodeIdFromUrl() {
    // Extrait l'ID de l'URL /web/node/{id}
    const match = window.location.pathname.match(/\/node\/([^\/]+)/);
    return match ? match[1] : null;
}


// --- React-like state and render functions ---
const state = {
    agent: null,
    lastReport: null,
    reports: [],
    error: null,
    tags: {},
};
function renderAgentTags() {
    const isAdmin = window.currentUser && window.currentUser.role === 'admin';
    let html = '';
    if (!state.tags || Object.keys(state.tags).length === 0) {
        html = '<em>Aucun tag</em>';
    } else {
        html = '<ul class="list-group mb-2" style="max-width:480px;">';
        for (const [k, values] of Object.entries(state.tags)) {
            if (Array.isArray(values)) {
                for (const v of values) {
                    html += `<li class="list-group-item py-1 px-2 d-flex justify-content-between align-items-center" style="font-size:0.97em;">
                        <span><span class="badge badge-secondary mr-2" style="font-size:0.93em;">${k}</span> <span>${v}</span></span>` + (isAdmin ? `<button class="btn btn-outline-danger btn-xs delete-tag-btn" data-key="${k}" data-value="${v}" style="padding:2px 8px;font-size:0.92em;">✕</button>` : '') + `
                    </li>`;
                }
            } else {
                html += `<li class="list-group-item py-1 px-2 d-flex justify-content-between align-items-center" style="font-size:0.97em;">
                    <span><span class="badge badge-secondary mr-2" style="font-size:0.93em;">${k}</span> <span>${values}</span></span>` + (isAdmin ? `<button class="btn btn-outline-danger btn-xs delete-tag-btn" data-key="${k}" data-value="${values}" style="padding:2px 8px;font-size:0.92em;">✕</button>` : '') + `
                </li>`;
            }
        }
        html += '</ul>';
    }
    return html;
}

function fillAgentInfo() {
    if (!state.agent) {
        $('#agent-name').text('Error loading');
        $('#agent-thumbprint').text('');
        $('#agent-lastcomm').text('');
        $('#agent-config-block').html('');
        return;
    }
    let certExpRaw = state.agent.certificate_notafter || '';
    let certExp = certExpRaw;
    let daysLeft = '';
    if (certExpRaw) {
        let expDate = new Date(String(certExpRaw).replace(/\.(\d{3})\d+/, '.$1'));
        if (!isNaN(expDate.getTime())) {
            let y = expDate.getFullYear();
            let m = (expDate.getMonth()+1).toString().padStart(2,'0');
            let d = expDate.getDate().toString().padStart(2,'0');
            let h = expDate.getHours().toString().padStart(2,'0');
            let min = expDate.getMinutes().toString().padStart(2,'0');
            let s = expDate.getSeconds().toString().padStart(2,'0');
            certExp = `${y}-${m}-${d} ${h}:${min}:${s}`;
            let now = new Date();
            let diffMs = expDate - now;
            let diffDays = Math.ceil(diffMs / (1000 * 60 * 60 * 24));
            daysLeft = ` (${diffDays} day${diffDays === 1 ? '' : 's'} remaining)`;
        }
    }
    let lastCommRaw = state.agent.last_communication || '';
    let lastComm = lastCommRaw;
    if (lastCommRaw) {
        let dt = new Date(lastCommRaw);
        if (!isNaN(dt.getTime())) {
            let y = dt.getFullYear();
            let m = (dt.getMonth()+1).toString().padStart(2,'0');
            let d = dt.getDate().toString().padStart(2,'0');
            let h = dt.getHours().toString().padStart(2,'0');
            let min = dt.getMinutes().toString().padStart(2,'0');
            let s = dt.getSeconds().toString().padStart(2,'0');
            lastComm = `${y}-${m}-${d} ${h}:${min}:${s}`;
        }
    }
    $('#agent-name').text(state.agent.node_name || '');
    if (state.agent.deleted_at) {
        $('#agent-name').append(' <span class="badge bg-dark">Corbeille</span>');
    }
    $('#agent-thumbprint').text(certExp + daysLeft);
    $('#agent-lastcomm').text(lastComm);
    if (Array.isArray(state.agent.configurations) && state.agent.configurations.length > 0) {
        let html = `<strong>Configuration${state.agent.configurations.length > 1 ? 's' : ''} associée${state.agent.configurations.length > 1 ? 's' : ''} :</strong> ` +
            state.agent.configurations.map(c => `<span class="badge badge-info mr-1">${c}</span>`).join(' ');
        $('#agent-config-block').html(html);
    } else {
        $('#agent-config-block').html('');
    }
}

function renderSelectedReport() {
    const report = state.selectedReport;
    if (!report) {
        return `<p><strong>Status:</strong> <span id="report-status">No report</span></p>
                <div><strong>Start Time:</strong> <span id="report-starttime"></span></div>
                <div><strong>Operation Type:</strong> <span id="report-operationtype"></span></div>
                <p><strong>Errors:</strong> <span id="report-errors"></span></p>
                <pre id="report-json" style="background:#eee; padding:10px;"></pre>`;
    }
    // Extraction des ressources en/desired state depuis StatusData
    let resourcesInDesired = [];
    let resourcesNotInDesired = [];
    if (Array.isArray(report.StatusData) && report.StatusData.length > 0) {
        for (const sd of report.StatusData) {
            try {
                const obj = typeof sd === 'string' ? JSON.parse(sd) : sd;
                if (Array.isArray(obj.ResourcesInDesiredState)) {
                    resourcesInDesired = resourcesInDesired.concat(obj.ResourcesInDesiredState);
                }
                if (Array.isArray(obj.ResourcesNotInDesiredState)) {
                    resourcesNotInDesired = resourcesNotInDesired.concat(obj.ResourcesNotInDesiredState);
                }
            } catch(e) {}
        }
    }
    function renderResourceTable(resources, title) {
        if (!Array.isArray(resources) || resources.length === 0) return '';
        let cols = Object.keys(resources[0] || {});
        // Retire la colonne StartDate, ModuleVersion, ResourceId et SourceInfo si présentes
        cols = cols.filter(c => c !== 'StartDate' && c !== 'ModuleVersion' && c !== 'ResourceId' && c !== 'SourceInfo');
        let html = `<p><strong>${title}</strong></p><table style="width:100%" class="table table-bordered table-sm"><thead><tr>`;
        html += cols.map(c => `<th>${c}</th>`).join('');
        html += '</tr></thead><tbody>';
        const isNotDesired = title.toLowerCase().includes('not in desired');
        for(const [rowIdx, res] of resources.entries()) {
            html += '<tr>' + cols.map(c => {
                if (c === 'ModuleName') {
                    let mod = res[c] || '';
                    let version = res['ModuleVersion'] || '';
                    if (mod) {
                        return `<td>${mod}${version ? '<span class="badge badge-info" style="font-size:90%; margin-left:6px;">' + version + '</span>' : ''}</td>`;
                    } else {
                        return `<td></td>`;
                    }
                } else if (c.toLowerCase() === 'error' && isNotDesired) {
                    let err = res[c] || '';
                    let shortErr = err.length > 60 ? err.substring(0, 60) + '…' : err;
                    let btn = err ? `<a href="#" class="show-error-modal" data-error="${encodeURIComponent(err)}" data-row="${rowIdx}">${shortErr}</a>` : '';
                    return `<td>${btn}</td>`;
                } else {
                    return `<td>${res[c] || ''}</td>`;
                }
            }).join('') + '</tr>';
        }
        html += '</tbody></table>';
        return html;
    }
    // Statut et infos génériques
    let statusValue = report.Status || report.status || '';
    let statusText = statusValue ? statusValue : 'Unknown';
    let statusClass = '';
    if (statusValue.toLowerCase() === 'success') {
        statusClass = 'alert alert-success';
    } else if (statusValue) {
        statusClass = 'alert alert-danger';
    } else {
        statusClass = 'alert alert-secondary';
    }
    // Robust extraction of StartTime/EndTime
    let startTime = report.StartTime || report.startTime || '';
    let endTime = report.EndTime || report.endTime || '';
    if (!startTime && Array.isArray(report.StatusData) && report.StatusData.length > 0) {
        try {
            const statusObj = JSON.parse(report.StatusData[0]);
            startTime = statusObj.StartDate || '';
        } catch(e) {}
    }
    if (!startTime && typeof state.selectedReportIdx === 'number' && state.reports[state.selectedReportIdx]) {
        startTime = state.reports[state.selectedReportIdx].created_at || '';
    }
    let operationType = report.OperationType || report.operationType || '';

    // Partie détails/erreurs
    let hasErrors = Array.isArray(report.Errors) && report.Errors.length > 0;
    let errorsHtml = '';
    if (hasErrors) {
        let table = '<table class="table table-bordered table-sm"><thead><tr>';
        let cols = [];
        try {
            const first = JSON.parse(report.Errors[0]);
            cols = Object.keys(first);
            table += cols.map(c => `<th>${c}</th>`).join('');
            table += '</tr></thead><tbody>';
            for(const errStr of report.Errors) {
                try {
                    const err = JSON.parse(errStr);
                    table += '<tr>' + cols.map(c => `<td>${err[c] || ''}</td>`).join('') + '</tr>';
                } catch(e) {
                    table += `<tr><td colspan="${cols.length}">${errStr}</td></tr>`;
                }
            }
            table += '</tbody></table>';
        } catch(e) {
            table = report.Errors.join('<br>');
        }
        errorsHtml = `<p><strong>Errors:</strong></p>${table}`;
    }

    return `
        <div class="${statusClass}" style="padding:8px 12px;"><strong>Status:</strong> <span id="report-status">${statusText}</span></div>
        <div><strong>Start Time:</strong> <span id="report-starttime">${startTime}</span></div>
        <div><strong>End Time:</strong> <span id="report-endtime">${endTime}</span></div>
        <div><strong>Operation Type:</strong> <span id="report-operationtype">${operationType}</span></div>
        <hr>
        ${renderResourceTable(resourcesInDesired, 'Resources In Desired State')}
        ${renderResourceTable(resourcesNotInDesired, 'Resources Not In Desired State')}
        ${errorsHtml}
        <div style="margin-top:1em;">
            <button id="toggle-raw-json" class="btn btn-outline-secondary btn-sm" type="button" style="margin-bottom:5px;">Afficher les données brutes</button>
            <div id="raw-json-block" style="display:none; max-width:100%; overflow-x:auto;">
                <pre id="report-json" style="background:#eee; padding:10px;">${JSON.stringify(report, null, 2)}</pre>
            </div>
        </div>
                <script>
                        $(function(){
                                $('#toggle-raw-json').off('click').on('click', function() {
                                        const block = $('#raw-json-block');
                                        if(block.is(':visible')) {
                                                block.slideUp(150);
                                                $(this).text('Afficher les données brutes');
                                        } else {
                                                block.slideDown(150);
                                                $(this).text('Masquer les données brutes');
                                        }
                                });
                                // Gestion modale erreur détaillée
                                $(document).off('click', '.show-error-modal').on('click', '.show-error-modal', function(e) {
                                        e.preventDefault();
                                        var err = decodeURIComponent($(this).data('error'));
                                        if($('#errorDetailModal').length === 0) {
                                                var modalHtml = '<div class="modal fade" id="errorDetailModal" tabindex="-1" role="dialog" aria-labelledby="errorDetailModalLabel" aria-hidden="true">'+
                                                    '<div class="modal-dialog modal-lg" role="document">'+
                                                        '<div class="modal-content">'+
                                                            '<div class="modal-header bg-danger text-white">'+
                                                                '<h5 class="modal-title" id="errorDetailModalLabel">Erreur complète</h5>'+ 
                                                                '<button type="button" class="close text-white" data-dismiss="modal" aria-label="Close">'+
                                                                    '<span aria-hidden="true">&times;</span>'+ 
                                                                '</button>'+ 
                                                            '</div>'+ 
                                                            '<div class="modal-body" id="errorDetailModalBody" style="background:#222;color:#fff;white-space:pre-wrap;font-family:monospace;font-size:1em;max-height:70vh;overflow:auto;border-radius:6px;padding:18px 16px;"></div>'+ 
                                                        '</div>'+ 
                                                    '</div>'+ 
                                                '</div>';
                                                $('body').append(modalHtml);
                                        }
                                        // Formatage JSON si possible
                                        let formatted = err;
                                        try {
                                                let obj = JSON.parse(err);
                                                formatted = JSON.stringify(obj, null, 2);
                                        } catch(e) {}
                                        $('#errorDetailModalBody').text(formatted);
                                        $('#errorDetailModal').modal('show');
                                });
                        });
                </script>
    `;
}

function renderReportsDropdown() {
    if (!Array.isArray(state.reports) || state.reports.length === 0) {
        return '<em>No report available.</em>';
    }
    let html = '<label for="report-select">Select a report:</label> ';
    html += '<select id="report-select" class="form-control form-control-sm" style="width:auto; display:inline-block; margin-left:10px;">';
    state.reports.forEach((rep, idx) => {
        const label = rep.created_at || '';
        let status = (rep.Status || rep.status || '').toLowerCase();
        let bgColor = '';
        if (status === 'success') {
            bgColor = 'background-color:#d4edda; color:#155724;'; // vert pâle
        } else if (status) {
            bgColor = 'background-color:#f8d7da; color:#721c24;'; // rouge pâle
        } else {
            bgColor = 'background-color:#e9ecef; color:#495057;'; // gris clair
        }
        html += `<option value="${idx}"${state.selectedReportIdx === idx ? ' selected' : ''} style="${bgColor}">${label}</option>`;
    });
    html += '</select>';
    return html;
}

function renderStatusHistory() {
    var canvas = document.getElementById('status-history-chart');
    if (!canvas) return;
    if (!Array.isArray(state.reports) || state.reports.length === 0) {
        if (window.statusHistoryChart) { window.statusHistoryChart.destroy(); window.statusHistoryChart = null; }
        $(canvas).hide();
        $('#status-history-empty').show();
        return;
    }
    $('#status-history-empty').hide();
    $(canvas).show();

    // Les rapports sont triés par created_at DESC : on les remet en ordre chronologique.
    var reps = state.reports.slice().reverse();
    var labels = [];
    var colors = [];
    var tooltips = [];
    reps.forEach(function(rep) {
        var ts = rep.created_at || '';
        var status = (rep.Status || rep.status || '').toLowerCase();
        labels.push(ts ? formatStatusDate(ts) : '');
        colors.push(status === 'success' ? '#28a745' : '#dc3545');
        tooltips.push((rep.Status || rep.status || '') + '  ' + ts);
    });

    var ctx = canvas.getContext('2d');
    if (window.statusHistoryChart) { window.statusHistoryChart.destroy(); }
    window.statusHistoryChart = new Chart(ctx, {
        type: 'bar',
        data: {
            labels: labels,
            datasets: [{
                data: reps.map(function() { return 1; }),
                backgroundColor: colors,
                borderColor: colors,
                borderWidth: 1,
                borderRadius: 3,
                barPercentage: 0.75
            }]
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            plugins: {
                legend: { display: false },
                tooltip: {
                    callbacks: {
                        title: function(ctx) { return tooltips[ctx[0].dataIndex] || ''; },
                        label: function() { return ''; }
                    }
                }
            },
            scales: {
                y: { display: false, max: 1 },
                x: {
                    ticks: { maxRotation: 45, minRotation: 0, autoSkip: true, maxTicksLimit: 12 },
                    grid: { display: false }
                }
            }
        }
    });
}

function formatStatusDate(ts) {
    var t = String(ts).replace('T', ' ').replace('Z', '');
    if (/^\d{4}-\d{2}-\d{2}/.test(t)) {
        var d = new Date(t.replace(' ', 'T') + 'Z');
        if (!isNaN(d.getTime())) {
            return d.toLocaleDateString() + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
        }
    }
    return ts;
}

function renderAll() {
    fillAgentInfo();
    $('#agent-tags-block').html(renderAgentTags());
    $('#reports-dropdown-block').html(renderReportsDropdown());
    $('#last-report-block').html(renderSelectedReport());
    $('#node-action-block').html(renderNodeActionBlock());
    renderStatusHistory();
}

$(document).ready(function() {
    const agentId = getNodeIdFromUrl();
    // Ajout/suppression de tags réservé aux admins : on n'affiche le formulaire
    // que pour un utilisateur admin.
    if (window.currentUser && window.currentUser.role === 'admin') {
        $('#add-tag-form').show();
    }
    // Ne pas réécrire le HTML, juste remplir les valeurs
    renderAll();
    if (!agentId) {
        state.error = 'Agent ID not found in URL';
        renderAll();
        return;
    }
    // Load agent info (utilise $.ajax pour profiter de la config globale JWT)
    $.ajax({
        url: `/api/v1/agents/${agentId}`,
        method: 'GET',
        dataType: 'json',
        success: function(agent) {
            state.agent = agent;
            renderAll();
        },
        error: function() {
            state.agent = null;
            renderAll();
        }
    });
    // Load tags
    $.ajax({
        url: `/api/v1/agents/${agentId}/tags`,
        method: 'GET',
        dataType: 'json',
        success: function(tags) {
            state.tags = tags || {};
            renderAll();
        },
        error: function() {
            state.tags = {};
            renderAll();
        }
    });
        // Ajout d'un tag
        $(document).on('submit', '#add-tag-form', function(e) {
            e.preventDefault();
            const key = $('#tag-key').val().trim();
            const value = $('#tag-value').val().trim();
            if (!key || !value) return;
            $.ajax({
                url: `/api/v1/agents/${agentId}/tags`,
                method: 'PUT',
                contentType: 'application/json',
                data: JSON.stringify({ key, value }),
                success: function() {
                    $('#tag-key').val('');
                    $('#tag-value').val('');
                    // Reload tags
                    $.ajax({
                        url: `/api/v1/agents/${agentId}/tags`,
                        method: 'GET',
                        dataType: 'json',
                        success: function(tags) {
                            state.tags = tags || {};
                            renderAll();
                        }
                    });
                }
            });
        });
        // Suppression d'une valeur de tag
        $(document).on('click', '.delete-tag-btn', function() {
            const key = $(this).data('key');
            const value = $(this).data('value');
            if (!key || !value) return;
            if (!window.confirm(`Supprimer le tag "${key}: ${value}" ?`)) return;
            $.ajax({
                url: `/api/v1/agents/${agentId}/tags?key=` + encodeURIComponent(key) + `&value=` + encodeURIComponent(value),
                method: 'DELETE',
                success: function() {
                    // Reload tags
                    $.ajax({
                        url: `/api/v1/agents/${agentId}/tags`,
                        method: 'GET',
                        dataType: 'json',
                        success: function(tags) {
                            state.tags = tags || {};
                            renderAll();
                        }
                    });
                }
            });
        });
    // Charger la liste des rapports
    $.ajax({
        url: `/api/v1/agents/${agentId}/reports?mofapplied=1`,
        method: 'GET',
        dataType: 'json',
        success: function(reports) {
            state.reports = reports;
            state.selectedReportIdx = 0;
            renderAll();
            setTimeout(() => {
                $('#report-select').trigger('change');
            }, 0);
        },
        error: function() {
            state.reports = [];
            state.selectedReportIdx = null;
            state.selectedReport = null;
            renderAll();
        }
    });

    // Export CSV des rapports du nœud (blob authentifié).
    $(document).on('click', '#export-reports-csv-btn', function() {
        const url = `/api/v1/agents/${agentId}/reports/export`;
        const xhr = new XMLHttpRequest();
        xhr.open('GET', url, true);
        xhr.responseType = 'blob';
        xhr.onload = function() {
            if(xhr.status === 200) {
                const blob = xhr.response;
                const link = document.createElement('a');
                link.href = window.URL.createObjectURL(blob);
                link.download = 'agent_reports_export.csv';
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

    // Gestion du changement de sélection de rapport (delegation)
    $(document).on('change', '#report-select', function() {
        const idx = parseInt($(this).val(), 10);
        state.selectedReportIdx = idx;
        const rep = state.reports[idx];
        if (rep && rep.job_id) {
            // Aller chercher le rapport détaillé
            $.ajax({
                url: `/api/v1/agents/${agentId}/reports/${rep.job_id}`,
                method: 'GET',
                dataType: 'json',
                success: function(report) {
                    state.selectedReport = report;
                    renderAll();
                },
                error: function() {
                    state.selectedReport = null;
                    renderAll();
                }
            });
        } else {
            state.selectedReport = rep || null;
            renderAll();
        }
    });

       // Bouton gestion propriétés du nœud
       $(document).on('click', '#node-properties-link', function(e) {
           e.preventDefault();
           if(agentId) {
               window.location.href = '/web/node/' + encodeURIComponent(agentId) + '/properties';
           }
       });
});
