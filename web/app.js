
$(document).ready(function() {
    $('#agents-table').DataTable({
        ajax: {
            url: '/api/v1/agents',
            dataSrc: ''
        },
        columns: [
            { data: 'agent_id' },
            { data: 'node_name' },
            { data: 'lcm_version' },
            { data: 'registration_type' },
            { data: 'certificate_thumbprint' },
            { data: 'registered_at' },
            {
                data: 'agent_id',
                render: function(data, type, row) {
                    return `<button onclick="showConfigs('${data}')">Configs</button>`;
                }
            },
            {
                data: 'agent_id',
                render: function(data, type, row) {
                    return `<button onclick="showReports('${data}')">Reports</button>`;
                }
            }
        ]
    });
});


async function showConfigs(agentId) {
    document.getElementById('agents-section').style.display = 'none';
    document.getElementById('configs-section').style.display = '';
    document.getElementById('selected-agent-id').textContent = agentId;
    await loadConfigs(agentId);
}

async function loadConfigs(agentId) {
    const res = await fetch(`/api/v1/agents/${agentId}/configs`);
    const configs = await res.json();
    const ul = document.getElementById('configs-list');
    ul.innerHTML = '';
    configs.forEach(cfg => {
        const li = document.createElement('li');
        li.textContent = cfg + ' ';
        const delBtn = document.createElement('button');
        delBtn.textContent = 'Delete';
        delBtn.onclick = async function() {
            if(confirm('Delete the configuration "' + cfg + '" ?')) {
                await fetch(`/api/v1/agents/${agentId}/configs`, {
                    method: 'DELETE',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ configuration_name: cfg })
                });
                await loadConfigs(agentId);
            }
        };
        li.appendChild(delBtn);
        ul.appendChild(li);
    });
// Add configuration
document.getElementById('add-config-form').onsubmit = async function(e) {
    e.preventDefault();
    const agentId = document.getElementById('selected-agent-id').textContent;
    const configName = document.getElementById('configName').value.trim();
    if(!configName) return;
    await fetch(`/api/v1/agents/${agentId}/configs`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ configuration_name: configName })
    });
    document.getElementById('configName').value = '';
    await loadConfigs(agentId);
};
}

function backToAgents() {
    document.getElementById('configs-section').style.display = 'none';
    document.getElementById('reports-section').style.display = 'none';
    document.getElementById('agents-section').style.display = '';
}

window.showReports = async function(agentId) {
    document.getElementById('agents-section').style.display = 'none';
    document.getElementById('configs-section').style.display = 'none';
    document.getElementById('reports-section').style.display = '';
    document.getElementById('reports-agent-id').textContent = agentId;
    await loadReports(agentId);
}

async function loadReports(agentId) {
    // Liste des rapports
    const res = await fetch(`/api/v1/agents/${agentId}/reports`);
    const reports = await res.json();
    const ul = document.getElementById('reports-list');
    ul.innerHTML = '';
    reports.forEach(rep => {
        const li = document.createElement('li');
        li.textContent = `JobId: ${rep.job_id} | Date: ${rep.created_at}`;
        const viewBtn = document.createElement('button');
        viewBtn.textContent = 'View';
        viewBtn.onclick = async function() {
            const r = await fetch(`/api/v1/agents/${agentId}/reports/${rep.job_id}`);
            const json = await r.json();
            document.getElementById('latest-report-json').textContent = JSON.stringify(json, null, 2);
        };
        li.appendChild(viewBtn);
        ul.appendChild(li);
    });
    // Dernier rapport
    const latestRes = await fetch(`/api/v1/agents/${agentId}/reports/latest`);
    if(latestRes.ok) {
        const latest = await latestRes.json();
        document.getElementById('latest-report-json').textContent = JSON.stringify(latest, null, 2);
    } else {
        document.getElementById('latest-report-json').textContent = 'No reports.';
    }
}



