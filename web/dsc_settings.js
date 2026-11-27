// web/dsc_settings.js
// Charge et sauvegarde les réglages DSC globaux via l'API admin /api/v1/dsc/settings.
$(function() {
    const $status = $('#dsc-settings-status');

    function showStatus(text, color) {
        $status.text(text).css('color', color);
    }

    function normalizeSettings(s) {
        const c = s || {};
        return {
            certificate_rotation_enabled: !!c.certificate_rotation_enabled,
            certificate_renewal_days: c.certificate_renewal_days || 30,
            reregistration_mode: c.reregistration_mode || 'deny'
        };
    }

    function loadSettings() {
        $.get('/api/v1/dsc/settings')
            .done(function(s) {
                const cfg = normalizeSettings(s);
                $('#reregistration_mode').val(cfg.reregistration_mode);
                $('#certificate_rotation_enabled').prop('checked', cfg.certificate_rotation_enabled);
                $('#certificate_renewal_days').val(cfg.certificate_renewal_days);
            })
            .fail(function() {
                showStatus('Unable to load settings.', 'red');
            });
    }

    $('#dsc-settings-form').on('submit', function(e) {
        e.preventDefault();
        const renewDays = parseInt($('#certificate_renewal_days').val(), 10);
        if (!renewDays || renewDays <= 0) {
            showStatus('Certificate renewal window must be > 0.', 'red');
            return;
        }
        const payload = {
            certificate_rotation_enabled: $('#certificate_rotation_enabled').prop('checked'),
            certificate_renewal_days: renewDays,
            reregistration_mode: $('#reregistration_mode').val()
        };
        $.ajax({
            url: '/api/v1/dsc/settings',
            method: 'PUT',
            contentType: 'application/json',
            data: JSON.stringify(payload)
        })
            .done(function() {
                showStatus('Settings saved.', 'green');
                loadSettings();
            })
            .fail(function(xhr) {
                const msg = (xhr.responseJSON && xhr.responseJSON.error) ? xhr.responseJSON.error : 'Failed to save settings.';
                showStatus(msg, 'red');
            });
    });

    loadSettings();
});
