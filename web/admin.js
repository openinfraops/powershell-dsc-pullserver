$(document).ready(function() {
    // Charger la liste des agents et remplir le DataTable
    $('#agents-table').DataTable({
        ajax: {
            url: '/api/v1/agents',
            dataSrc: ''
        },
        columns: [
            { data: 'node_name', title: 'Name' },
            { data: 'last_communication', title: 'Last Communication' },
            { data: 'thumbprint', title: 'Thumbprint' },
            { data: 'has_error_last_report', title: 'Status' },
            {
                data: 'agent_id',
                title: 'Detail',
                render: function(data, type, row) {
                    return `<button onclick="viewAgentDetail('${data}')">View</button>`;
                }
            }
        ]
    });
});

window.viewAgentDetail = function(agentId) {
    // Afficher le panneau de détail
    $('#agent-detail-modal').show();
    // Charger les infos agent
    fetch(`/api/v1/agents/${agentId}`).then(r => r.json()).then(agent => {
        $('#agent-detail-name').text(agent.node_name);
        $('#agent-detail-thumbprint').text(agent.thumbprint);
        $('#agent-detail-lastcomm').text(agent.last_communication);
    });
    // Charger le dernier rapport
    fetch(`/api/v1/agents/${agentId}/reports/latest`).then(r => r.json()).then(report => {
        if(report && report.status) {
            $('#agent-detail-report-status').text(report.status);
            $('#agent-detail-report-errors').text(report.errors && report.errors.length ? report.errors.join(', ') : 'No errors');
        } else {
            $('#agent-detail-report-status').text('No report');
            $('#agent-detail-report-errors').text('');
        }
        $('#agent-detail-report-json').text(JSON.stringify(report, null, 2));
    }).catch(() => {
        $('#agent-detail-report-status').text('No report');
        $('#agent-detail-report-errors').text('');
        $('#agent-detail-report-json').text('');
    });
}

window.closeAgentDetail = function() {
    $('#agent-detail-modal').hide();
}
