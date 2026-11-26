// error_groups.js — page /web/errors (Erreurs DSC)
// Consomme GET/PUT /api/v1/error_groups.
// Le JWT est envoyé automatiquement via le cookie HttpOnly (jwt_ajax.js).

const egState = {
    groups: [],
    total: 0,
    offset: 0,
    limit: 20,
    currentFingerprint: null,
    detail: null,
    nodes: [],
};

function egIsAdmin() {
    return !!(window.currentUser && window.currentUser.role === 'admin');
}

function egEsc(s) {
    if (s === null || s === undefined) return '';
    return String(s)
        .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function egFmtDate(val) {
    if (!val) return '—';
    let d = new Date(val);
    if (isNaN(d.getTime())) return egEsc(val);
    let y = d.getFullYear();
    let m = String(d.getMonth()+1).padStart(2,'0');
    let day = String(d.getDate()).padStart(2,'0');
    let h = String(d.getHours()).padStart(2,'0');
    let min = String(d.getMinutes()).padStart(2,'0');
    return `${y}-${m}-${day} ${h}:${min}`;
}

function egStatusBadge(status) {
    const map = {
        open:     ['badge-warning', 'Open'],
        known:    ['badge-info', 'Known'],
        resolved: ['badge-success', 'Resolved'],
    };
    const entry = map[status] || ['badge-secondary', status || '—'];
    return `<span class="badge ${entry[0]}">${egEsc(entry[1])}</span>`;
}

function egDetectionBadges(g) {
    const parts = [];
    if (g.new) parts.push('<span class="badge badge-danger">New</span>');
    if (g.spike) parts.push('<span class="badge badge-danger">Spike</span>');
    if (g.after_deployment) parts.push('<span class="badge badge-primary">Post-deployment</span>');
    if (g.trend) parts.push(`<span class="badge badge-secondary" title="Trend">${egEsc(g.trend)}</span>`);
    if (g.detection_suppressed) parts.push('<span class="badge badge-light">Dismissed</span>');
    if (parts.length === 0) return '—';
    return parts.join(' ');
}

function egBuildQuery() {
    const q = [];
    const status = $('#eg-filter-status').val();
    const category = $('#eg-filter-category').val().trim();
    const minNodes = $('#eg-filter-minnodes').val();
    const since = $('#eg-filter-since').val();
    if (status) q.push('status=' + encodeURIComponent(status));
    if (category) q.push('category=' + encodeURIComponent(category));
    if (minNodes && minNodes !== '0') q.push('min_nodes=' + encodeURIComponent(minNodes));
    if (since) q.push('since=' + encodeURIComponent(since));
    return q;
}

function egLoadGroups(offset) {
    if (typeof offset === 'number') egState.offset = offset;
    const params = egBuildQuery();
    params.push('offset=' + egState.offset);
    params.push('limit=' + egState.limit);
    const url = '/api/v1/error_groups?' + params.join('&');
    $('#eg-count').html('<span class="spinner-border spinner-border-sm"></span> Loading…');
    $.ajax({
        url: url,
        method: 'GET',
        dataType: 'json',
        success: function(resp) {
            egState.groups = resp.groups || [];
            egState.total = resp.total || 0;
            egState.offset = resp.offset || 0;
            egRenderTable();
            egRenderPagination();
        },
        error: function(xhr) {
            $('#eg-count').html('<span class="text-danger">Load error: ' + egEsc(xhr.responseText || xhr.status) + '</span>');
        }
    });
}

function egRenderTable() {
    const $tbody = $('#eg-tbody').empty();
    if (!egState.groups.length) {
        $tbody.html('<tr><td colspan="9" class="text-center text-muted">No error groups.</td></tr>');
        $('#eg-count').text(egState.total + ' error group(s).');
        return;
    }
    egState.groups.forEach(function(g) {
        const shortMsg = g.sample_message && g.sample_message.length > 120
            ? g.sample_message.substring(0, 120) + '…' : (g.sample_message || '');
        const tr = $('<tr>').css('cursor', 'pointer').attr('data-fp', egEsc(g.fingerprint));
        tr.append($('<td>').html('<code>' + egEsc((g.fingerprint || '').substring(0, 12)) + '…</code>'));
        tr.append($('<td>').text(g.category || '—'));
        tr.append($('<td>').html(egEsc(shortMsg)));
        tr.append($('<td>').html(egStatusBadge(g.status)));
        tr.append($('<td>').text(g.node_count));
        tr.append($('<td>').text(g.node_count_last_hour + ' / ' + g.node_count_prev_hour));
        tr.append($('<td>').html(egDetectionBadges(g)));
        tr.append($('<td>').text(egFmtDate(g.first_seen)));
        tr.append($('<td>').text(egFmtDate(g.last_seen)));
        $tbody.append(tr);
    });
    $('#eg-count').text(egState.total + ' error group(s).');
}

function egRenderPagination() {
    const $p = $('#eg-pagination').empty();
    if (egState.total <= egState.limit) return;
    const prev = egState.offset > 0;
    const next = (egState.offset + egState.limit) < egState.total;
    let html = '<div class="d-flex justify-content-between align-items-center">';
    html += '<span class="text-muted">' + (egState.offset + 1) + ' – ' + Math.min(egState.offset + egState.limit, egState.total) + ' / ' + egState.total + '</span>';
    html += '<div>';
    if (prev) html += '<button id="eg-prev" class="btn btn-outline-secondary btn-sm mr-1">Previous</button>';
    if (next) html += '<button id="eg-next" class="btn btn-outline-secondary btn-sm">Next</button>';
    html += '</div></div>';
    $p.html(html);
    $('#eg-prev').on('click', function() { egLoadGroups(Math.max(0, egState.offset - egState.limit)); });
    $('#eg-next').on('click', function() { egLoadGroups(egState.offset + egState.limit); });
}

function egOpenDetail(fingerprint) {
    egState.currentFingerprint = fingerprint;
    egState.detail = null;
    egState.nodes = [];
    $.ajax({
        url: '/api/v1/error_groups/' + encodeURIComponent(fingerprint),
        method: 'GET',
        dataType: 'json',
        success: function(g) {
            egState.detail = g;
            egRenderDetailModal();
        },
        error: function(xhr) {
            $('#eg-detail-body').html('<span class="text-danger">Error: ' + egEsc(xhr.responseText || xhr.status) + '</span>');
        }
    });
    $.ajax({
        url: '/api/v1/error_groups/' + encodeURIComponent(fingerprint) + '/nodes?limit=200',
        method: 'GET',
        dataType: 'json',
        success: function(resp) {
            egState.nodes = resp.nodes || [];
            egRenderDetailNodes();
        },
        error: function() { egState.nodes = []; egRenderDetailNodes(); }
    });
}

function egDetailHeaderHtml() {
    const g = egState.detail;
    const rows = [
        ['Fingerprint', '<code>' + egEsc(g.fingerprint) + '</code>'],
        ['Status', egStatusBadge(g.status)],
        ['Category', egEsc(g.category || '—')],
        ['Resource type', egEsc(g.resource_type || '—')],
        ['Affected nodes', String(g.node_count)],
        ['Detection', egDetectionBadges(g)],
        ['1h / previous 1h', g.node_count_last_hour + ' / ' + g.node_count_prev_hour],
        ['First seen', egFmtDate(g.first_seen)],
        ['Last seen', egFmtDate(g.last_seen)],
        ['First config version', egEsc(g.first_config_version || '—')],
    ];
    if (g.status_changed_by) rows.push(['Modified on', egFmtDate(g.status_changed_at) + ' by ' + egEsc(g.status_changed_by)]);
    let html = '<table class="table table-bordered table-sm mb-3">';
    rows.forEach(function(r) {
        html += '<tr><th style="width:180px;">' + egEsc(r[0]) + '</th><td>' + r[1] + '</td></tr>';
    });
    html += '</table>';
    return html;
}

function egDetailBodyHtml() {
    const g = egState.detail;
    let html = egDetailHeaderHtml();
    html += '<h6>Message</h6><pre style="white-space:pre-wrap;background:#f4f4f4;border-radius:4px;padding:10px;max-height:160px;overflow:auto;">' + egEsc(g.sample_message || '') + '</pre>';
    html += '<h6 class="mt-3">Note</h6>';
    if (egIsAdmin()) {
        html += '<textarea id="eg-note-input" class="form-control form-control-sm" rows="3" placeholder="Triage note…">' + egEsc(g.note || '') + '</textarea>';
    } else {
        html += '<div>' + (g.note ? egEsc(g.note) : '<em class="text-muted">No note.</em>') + '</div>';
    }
    html += '<h6 class="mt-3">Affected nodes</h6><div id="eg-nodes-block"></div>';
    return html;
}

function egRenderDetailModal() {
    if (!egState.detail) return;
    const g = egState.detail;
    $('#eg-detail-title').text('Error — ' + (g.category || ''));
    $('#eg-detail-body').html(egDetailBodyHtml());
    // Re-rend les nœuds si leur réponse est arrivée avant le détail (course
    // possible entre les deux appels AJAX de egOpenDetail).
    egRenderDetailNodes();
    let footer = '<button type="button" class="btn btn-outline-secondary btn-sm" data-dismiss="modal">Close</button>';
    if (egIsAdmin()) {
        footer += '<button id="eg-status-known" class="btn btn-info btn-sm ml-1">Mark as known</button>';
        footer += '<button id="eg-status-resolved" class="btn btn-success btn-sm ml-1">Resolve</button>';
        footer += '<button id="eg-status-open" class="btn btn-warning btn-sm ml-1">Reopen</button>';
        footer += '<button id="eg-save" class="btn btn-primary btn-sm ml-1">Save</button>';
    }
    $('#eg-detail-footer').html(footer);
    $('#eg-detail-modal').modal('show');
    if (egIsAdmin()) {
        $('#eg-status-known').on('click', function() { egChangeStatus('known'); });
        $('#eg-status-resolved').on('click', function() { egChangeStatus('resolved'); });
        $('#eg-status-open').on('click', function() { egChangeStatus('open'); });
        $('#eg-save').on('click', function() { egChangeStatus(null); });
    }
}

function egRenderDetailNodes() {
    const $nodes = $('#eg-nodes-block');
    if (!$nodes.length) return;
    if (!egState.nodes.length) {
        $nodes.html('<em class="text-muted">No nodes registered for this group.</em>');
        return;
    }
    let html = '<table class="table table-bordered table-sm"><thead><tr><th>Node</th><th>Occurrences</th><th>First</th><th>Last</th></tr></thead><tbody>';
    egState.nodes.forEach(function(o) {
        html += '<tr><td>' + egEsc(o.NodeID || o.node_id) + '</td><td>' + egEsc(String(o.Count || o.count)) + '</td><td>' + egFmtDate(o.FirstSeen || o.first_seen) + '</td><td>' + egFmtDate(o.LastSeen || o.last_seen) + '</td></tr>';
    });
    html += '</tbody></table>';
    $nodes.html(html);
}

function egChangeStatus(status) {
    const g = egState.detail;
    if (!g) return;
    const note = (typeof $('#eg-note-input').val() === 'string') ? $('#eg-note-input').val() : g.note;
    const body = { status: status || g.status, note: note || '' };
    $.ajax({
        url: '/api/v1/error_groups/' + encodeURIComponent(g.fingerprint),
        method: 'PUT',
        contentType: 'application/json',
        data: JSON.stringify(body),
        success: function() {
            egLoadGroups(egState.offset);
            egOpenDetail(g.fingerprint);
        },
        error: function(xhr) {
            alert('Error: ' + (xhr.responseText || xhr.status));
        }
    });
}

$(document).ready(function() {
    // Clic sur une ligne : ouvre le détail.
    $(document).on('click', '#eg-tbody tr[data-fp]', function() {
        egOpenDetail($(this).attr('data-fp'));
    });
    $('#eg-filter-apply').on('click', function() { egState.offset = 0; egLoadGroups(0); });
    $('#eg-filter-reset').on('click', function() {
        $('#eg-filter-status').val('');
        $('#eg-filter-category').val('');
        $('#eg-filter-minnodes').val('0');
        $('#eg-filter-since').val('');
        egState.offset = 0;
        egLoadGroups(0);
    });
    $('#eg-refresh').on('click', function() { egLoadGroups(egState.offset); });
    // Entrée dans un champ filtre déclenche aussi le filtrage.
    $('#eg-filter-category').on('keydown', function(e) { if (e.key === 'Enter') { e.preventDefault(); egState.offset = 0; egLoadGroups(0); } });
    $('#eg-filter-minnodes').on('keydown', function(e) { if (e.key === 'Enter') { e.preventDefault(); egState.offset = 0; egLoadGroups(0); } });
    egLoadGroups(0);
});
