$(document).ready(function() {
    var input = $('#global-search-input');
    if (!input.length) return;

    var resultsBox = $('#global-search-results');
    var debounceTimer = null;

    function esc(text) {
        if (text === null || text === undefined) return '';
        return String(text)
            .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
    }

    function renderResults(data) {
        var html = '';
        if (!data ||
            !(data.agents && data.agents.length) &&
            !(data.configurations && data.configurations.length) &&
            !(data.modules && data.modules.length)) {
            html = '<div class="dropdown-item text-muted">No results</div>';
            resultsBox.html(html).show();
            return;
        }

        if (data.agents && data.agents.length) {
            html += '<div class="dropdown-header">Nodes</div>';
            data.agents.forEach(function(a) {
                html += '<a class="dropdown-item" href="/web/node/' + encodeURIComponent(a.agent_id) + '">';
                html += '<i class="fas fa-server mr-2 text-info"></i>' + esc(a.node_name || a.agent_id);
                if (a.state) {
                    html += ' <span class="badge badge-secondary">' + esc(a.state) + '</span>';
                }
                html += '</a>';
            });
        }

        if (data.configurations && data.configurations.length) {
            html += '<div class="dropdown-divider"></div>';
            html += '<div class="dropdown-header">Configurations</div>';
            data.configurations.forEach(function(c) {
                html += '<a class="dropdown-item" href="/web/configuration_model/' + encodeURIComponent(c.name) + '">';
                html += '<i class="fas fa-file-alt mr-2 text-primary"></i>' + esc(c.name);
                if (c.uploaded_by) {
                    html += ' <span class="text-muted small">by ' + esc(c.uploaded_by) + '</span>';
                }
                html += '</a>';
            });
        }

        if (data.modules && data.modules.length) {
            html += '<div class="dropdown-divider"></div>';
            html += '<div class="dropdown-header">Modules</div>';
            data.modules.forEach(function(m) {
                html += '<a class="dropdown-item" href="/web/modules">';
                html += '<i class="fas fa-cubes mr-2 text-success"></i>' + esc(m.name);
                if (m.version) {
                    html += ' <span class="badge badge-light">' + esc(m.version) + '</span>';
                }
                html += '</a>';
            });
        }

        resultsBox.html(html).show();
    }

    function search(q) {
        q = (q || '').trim();
        if (q.length < 2) {
            resultsBox.hide();
            return;
        }
        $.getJSON('/api/v1/search?q=' + encodeURIComponent(q), function(data) {
            renderResults(data);
        }).fail(function() {
            resultsBox.html('<div class="dropdown-item text-danger">Search error</div>').show();
        });
    }

    input.on('input', function() {
        clearTimeout(debounceTimer);
        debounceTimer = setTimeout(function() { search(input.val()); }, 300);
    });

    input.on('keydown', function(e) {
        if (e.key === 'Escape') resultsBox.hide();
    });

    $(document).on('click', function(e) {
        if (!$(e.target).closest('#global-search-input, #global-search-results').length) {
            resultsBox.hide();
        }
    });

    $(document).on('click', '#global-search-results a', function() {
        resultsBox.hide();
    });
});
