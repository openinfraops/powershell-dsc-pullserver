$(function() {
    // Get the ID from the URL
    function getNameFromUrl() {
        var match = window.location.pathname.match(/configuration_model\/([^\/]+)/);
        return match ? decodeURIComponent(match[1]) : null;
    }
    var name = getNameFromUrl();
    if (!name) return;

    // --- Diff helpers ---------------------------------------------------------

    function escapeHtml(text) {
        if (text === null || text === undefined) return '';
        return String(text)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    }

    // Ligne à ligne : construit une matrice LCS puis produit les lignes
    // gauche/droite avec leur type (same / removed / added).
    function lcsDiff(a, b) {
        var n = a.length, m = b.length;
        var L = [];
        for (var i = 0; i <= n; i++) {
            L[i] = [];
            for (var j = 0; j <= m; j++) {
                if (i === 0 || j === 0) L[i][j] = 0;
                else if (a[i - 1] === b[j - 1]) L[i][j] = L[i - 1][j - 1] + 1;
                else L[i][j] = Math.max(L[i - 1][j], L[i][j - 1]);
            }
        }
        var rows = [];
        var i = n, j = m;
        while (i > 0 && j > 0) {
            if (a[i - 1] === b[j - 1]) {
                rows.push({ left: a[i - 1], right: b[j - 1], leftType: 'same', rightType: 'same' });
                i--; j--;
            } else if (L[i - 1][j] >= L[i][j - 1]) {
                rows.push({ left: a[i - 1], right: '', leftType: 'removed', rightType: '' });
                i--;
            } else {
                rows.push({ left: '', right: b[j - 1], leftType: '', rightType: 'added' });
                j--;
            }
        }
        while (i > 0) { rows.push({ left: a[i - 1], right: '', leftType: 'removed', rightType: '' }); i--; }
        while (j > 0) { rows.push({ left: '', right: b[j - 1], leftType: '', rightType: 'added' }); j--; }
        rows.reverse();
        return rows;
    }

    function renderDiff(diff) {
        var leftLines = String(diff.left || '').split('\n');
        var rightLines = String(diff.right || '').split('\n');
        var rows = lcsDiff(leftLines, rightLines);
        var html = '<div class="table-responsive"><table class="table table-sm table-bordered" style="font-family:monospace;font-size:0.85em;">';
        html += '<thead><tr>';
        html += '<th style="width:50%;word-break:break-all;">' + escapeHtml(diff.name) + '</th>';
        html += '<th style="width:50%;word-break:break-all;">' + escapeHtml(diff.other_name) + '</th>';
        html += '</tr></thead><tbody>';
        rows.forEach(function(row) {
            var leftStyle = row.leftType === 'removed' ? 'background-color:#f8d7da;'
                : (row.leftType === 'added' ? 'background-color:#d4edda;' : '');
            var rightStyle = row.rightType === 'removed' ? 'background-color:#f8d7da;'
                : (row.rightType === 'added' ? 'background-color:#d4edda;' : '');
            html += '<tr>';
            html += '<td style="white-space:pre-wrap;word-break:break-all;' + leftStyle + '">' + escapeHtml(row.left) + '</td>';
            html += '<td style="white-space:pre-wrap;word-break:break-all;' + rightStyle + '">' + escapeHtml(row.right) + '</td>';
            html += '</tr>';
        });
        html += '</tbody></table></div>';
        $('#diff-result-block').html(html);
    }

    // --- Load configuration details -------------------------------------------

    $.getJSON('/api/v1/configuration_models/' + encodeURIComponent(name) + '/detail', function(data) {
        var html = '';
        var isAdmin = window.currentUser && window.currentUser.role === 'admin';
        html += '<h3>' + escapeHtml(data.name) + '</h3>';
        html += '<p><b>Uploaded by:</b> ' + escapeHtml(data.uploaded_by) + ' | <b>Date:</b> ' + (data.upload_date ? new Date(data.upload_date).toLocaleString() : '') + '</p>';
        html += '<button class="btn btn-primary" id="download-mof-btn">Download MOF</button>';
        html += '<hr><h4>Version history</h4>';
        html += '<div style="font-family:monospace">';
        data.versions.forEach(function(v, idx) {
            var isCurrent = (v.name === data.name);
            html += '<div class="d-flex align-items-center" style="gap:6px;">';
            if(isCurrent) {
                html += '<span style="font-weight:bold;color:#222">&bull;</span> ';
                html += '<b>';
            } else {
                html += '<span style="color:#888">&#8593;</span> ';
            }
            html += '<a href="/web/configuration_model/' + encodeURIComponent(v.name) + '">' + escapeHtml(v.name) + '</a>';
            if(isCurrent) html += '</b>';
            html += ' <span style="color:#888">(' + (v.upload_date ? new Date(v.upload_date).toLocaleString() : '') + ')</span>';
            if(!isCurrent && isAdmin) {
                html += ' <button class="btn btn-outline-warning btn-sm ml-2 revert-version-btn" data-id="' + v.id + '" data-name="' + escapeHtml(v.name) + '" title="Republier cette version comme version active"><i class="fas fa-undo"></i> Revert</button>';
            }
            html += '</div>';
        });
        html += '</div>';

        // Zone de comparaison de deux versions (diff).
        if(data.versions && data.versions.length >= 2) {
            var currentId = data.id;
            var foundCurrent = data.versions.some(function(v) { return v.id === currentId; });
            var defaultLeft = foundCurrent ? currentId : data.versions[0].id;
            var defaultRight = null;
            for (var vi = 0; vi < data.versions.length; vi++) {
                if (data.versions[vi].id !== defaultLeft) { defaultRight = data.versions[vi].id; break; }
            }
            html += '<hr><h4>Compare versions</h4>';
            html += '<div class="form-inline mb-2">';
            html += '<select id="diff-left-select" class="form-control form-control-sm mr-2" style="max-width:240px;">';
            data.versions.forEach(function(v) {
                html += '<option value="' + v.id + '">' + escapeHtml(v.name) + '</option>';
            });
            html += '</select>';
            html += '<span class="mr-2">vs</span>';
            html += '<select id="diff-right-select" class="form-control form-control-sm mr-2" style="max-width:240px;">';
            data.versions.forEach(function(v) {
                html += '<option value="' + v.id + '">' + escapeHtml(v.name) + '</option>';
            });
            html += '</select>';
            html += '<button class="btn btn-primary btn-sm" id="compare-btn"><i class="fas fa-code-branch"></i> Compare</button>';
            html += '</div>';
            html += '<div id="diff-result-block"><span class="text-muted">Select two versions and click Compare.</span></div>';
            // Valeurs par défaut mémorisées pour être appliquées après l'insertion du HTML.
            window.__diffDefaults = { left: defaultLeft, right: defaultRight };
        }

        // Linked nodes display
        html += '<hr><h4>Nodes linked to this configuration</h4>';
        if(data.linked_nodes && data.linked_nodes.length) {
            html += '<div class="table-responsive"><table class="table table-sm table-bordered"><thead><tr><th>Agent ID</th><th>Node name</th><th>Status</th><th>Last communication</th></tr></thead><tbody>';
            data.linked_nodes.forEach(function(n) {
                html += '<tr>';
                html += '<td>' + escapeHtml(n.agent_id) + '</td>';
                html += '<td>' + escapeHtml(n.node_name || '') + '</td>';
                html += '<td>' + escapeHtml(n.state || '') + '</td>';
                html += '<td>' + (n.last_communication ? new Date(n.last_communication).toLocaleString() : '') + '</td>';
                html += '</tr>';
            });
            html += '</tbody></table></div>';
        } else {
            html += '<p>No linked nodes.</p>';
        }

        // Used modules display
        html += '<hr><h4>Used modules</h4>';
        if(data.modules && data.modules.length) {
            html += '<div class="table-responsive"><table class="table table-sm table-bordered"><thead><tr><th>Name</th><th>Required version</th><th>Available version</th></tr></thead><tbody>';
            data.modules.forEach(function(m, idx) {
                var modId = 'modver_' + idx;
                html += '<tr>';
                html += '<td>' + escapeHtml(m.name || '') + '</td>';
                html += '<td>' + escapeHtml(m.version || '') + '</td>';
                html += '<td id="' + modId + '"><span class="text-muted">Checking...</span></td>';
                html += '</tr>';
            });
            html += '</tbody></table></div>';
        } else {
            html += '<p>No modules detected.</p>';
        }
        $('#config-detail-container').html(html);

        // Valeurs par défaut des sélecteurs de diff, une fois le HTML présent.
        if(window.__diffDefaults) {
            $('#diff-left-select').val(window.__diffDefaults.left);
            $('#diff-right-select').val(window.__diffDefaults.right);
            delete window.__diffDefaults;
        }

        // Comparaison de versions
        $('#compare-btn').on('click', function() {
            var leftId = $('#diff-left-select').val();
            var rightId = $('#diff-right-select').val();
            if(!leftId || !rightId) { alert('Select two versions to compare.'); return; }
            if(leftId === rightId) { alert('Select two different versions.'); return; }
            $('#diff-result-block').html('<span class="text-muted">Computing diff...</span>');
            $.getJSON('/api/v1/configuration_models/' + encodeURIComponent(leftId) + '/diff?other=' + encodeURIComponent(rightId), function(diff) {
                renderDiff(diff);
            }).fail(function(xhr) {
                $('#diff-result-block').html('<div class="alert alert-danger mb-0">Diff failed: ' + escapeHtml(xhr.responseText || xhr.status) + '</div>');
            });
        });

        // MOF download with auth
        $('#download-mof-btn').on('click', function() {
            var url = '/api/v1/configuration_models/' + encodeURIComponent(data.id) + '/download';
            var xhr = new XMLHttpRequest();
            xhr.open('GET', url, true);
            xhr.responseType = 'blob';
            xhr.onload = function() {
                if(xhr.status === 200) {
                    var blob = xhr.response;
                    var link = document.createElement('a');
                    link.href = window.URL.createObjectURL(blob);
                    link.download = data.name + '.mof';
                    document.body.appendChild(link);
                    link.click();
                    document.body.removeChild(link);
                } else {
                    alert('Download failed: ' + xhr.status);
                }
            };
            xhr.onerror = function() { alert('Download error'); };
            xhr.send();
        });

        // Module version check
        if(data.modules && data.modules.length) {
            data.modules.forEach(function(m, idx) {
                var modId = 'modver_' + idx;
                if(!m.name) return;
                var url = '/api/v1/modules/' + encodeURIComponent(m.name);
                if(m.version) url += '?latest=1';
                $.getJSON(url, function(resp) {
                    // resp should contain {available: bool, version: string}
                    var cell = $('#' + modId);
                    if(resp && resp.available) {
                        var ok = true;
                        if(m.version && resp.version) {
                            // Simple version split/compare
                            var req = m.version.split('.').map(Number);
                            var got = resp.version.split('.').map(Number);
                            for(var i=0; i<req.length; ++i) {
                                if((got[i]||0) > req[i]) break;
                                if((got[i]||0) < req[i]) { ok = false; break; }
                            }
                        }
                        cell.html('<span style="color:'+(ok?'green':'red')+'">' + escapeHtml(resp.version) + '</span>');
                    } else {
                        $('#' + modId).html('<span style="color:red">Not available</span>');
                    }
                }).fail(function() {
                    $('#' + modId).html('<span style="color:red">Error</span>');
                });
            });
        }
    });

    // Revert : republier une version antérieure comme version active (admin).
    $(document).on('click', '.revert-version-btn', function() {
        var id = $(this).data('id');
        var versionName = $(this).data('name');
        if(!id) return;
        if(!confirm('Revert to version "' + versionName + '" ? Cette action republie cette version comme version active et forcera la recompilation des nœuds liés.')) return;
        $.ajax({
            url: '/api/v1/configuration_models/' + encodeURIComponent(id) + '/revert',
            method: 'POST',
            success: function(res) {
                if(res && res.status === 'already_active') {
                    alert('Cette version est déjà la version active.');
                    return;
                }
                alert('Configuration réactivée sur la version "' + (res && res.name ? res.name : versionName) + '".');
                window.location.reload();
            },
            error: function(xhr) {
                alert('Erreur revert: ' + (xhr.responseText || xhr.status));
            }
        });
    });
});
