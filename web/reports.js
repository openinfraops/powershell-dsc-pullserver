$(document).ready(function() {
    // Affiche le nombre total de configurations
    $.getJSON('/api/v1/configuration_models?count=1', function(data) {
        if (data && typeof data.count !== 'undefined') {
            $('#total-configurations').text(data.count);
        }
    });

    // Affiche le nombre total d'agents
    $.getJSON('/api/v1/agents?count=1', function(data) {
        if (data && typeof data.count !== 'undefined') {
            $('#total-agents').text(data.count);
        }
    });

    // Affiche le nombre total de modules
    $.getJSON('/api/v1/modules?count=1', function(data) {
        if (data && typeof data.count !== 'undefined') {
            $('#total-modules').text(data.count);
        }
    });

    // Camembert : répartition de l'état des agents (Configuration Status Overview)
    $.getJSON('/api/v1/agents?stats', function(stats) {
        let ok = (stats && typeof stats.compliant !== 'undefined') ? stats.compliant : 0;
        let err = (stats && typeof stats.failed !== 'undefined') ? stats.failed : 0;
        let pendingEnroll = (stats && typeof stats.pending_enroll !== 'undefined') ? stats.pending_enroll : 0;
        let pendingApply = (stats && typeof stats.pending_apply !== 'undefined') ? stats.pending_apply : 0;

        // Badge "certificats à risque" (expirés + expirant sous 30 jours)
        let certsExpired = (stats && typeof stats.certs_expired !== 'undefined') ? stats.certs_expired : 0;
        let certsExpiring = (stats && typeof stats.certs_expiring !== 'undefined') ? stats.certs_expiring : 0;
        let totalAtRisk = certsExpired + certsExpiring;
        if ($('#total-certs-at-risk').length) {
            $('#total-certs-at-risk').text(totalAtRisk > 0 ? totalAtRisk : '0');
        }

        const ctx = document.getElementById('agents-pie').getContext('2d');
        const chart = new Chart(ctx, {
            type: 'pie',
            data: {
                labels: ['Compliant', 'Failed', 'Pending Enroll', 'Pending Apply'],
                datasets: [{
                    data: [ok, err, pendingEnroll, pendingApply],
                    backgroundColor: ['#28a745', '#dc3545', '#6c757d', '#ffc107'],
                }]
            },
            options: {
                responsive: true,
                plugins: {
                    legend: { display: false }
                }
            }
        });

        // Gestion sélection légende custom
        function updatePieVisibility() {
            let showOk = $('#legend-ok').hasClass('active');
            let showErr = $('#legend-err').hasClass('active');
            let showPendingEnroll = $('#legend-pending-enroll').hasClass('active');
            let showPendingApply = $('#legend-pending-apply').hasClass('active');
            let newData = [showOk ? ok : 0, showErr ? err : 0, showPendingEnroll ? pendingEnroll : 0, showPendingApply ? pendingApply : 0];
            chart.data.datasets[0].data = newData;
            chart.update();
        }
        $('#legend-ok').on('click', function() {
            $(this).toggleClass('active');
            updatePieVisibility();
        });
        $('#legend-err').on('click', function() {
            $(this).toggleClass('active');
            updatePieVisibility();
        });
        $('#legend-pending-enroll').on('click', function() {
            $(this).toggleClass('active');
            updatePieVisibility();
        });
        $('#legend-pending-apply').on('click', function() {
            $(this).toggleClass('active');
            updatePieVisibility();
        });
        // Style visuel pour .active
        $('<style>.legend-selectable{opacity:0.6;} .legend-selectable.active{font-weight:bold;opacity:1;text-decoration:underline;}</style>').appendTo('head');
    });
});
