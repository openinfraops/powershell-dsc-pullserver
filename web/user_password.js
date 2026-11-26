$(function() {
    const urlParams = new URLSearchParams(window.location.search);
    const userId = urlParams.get('id');
    if (!userId) {
        window.location.href = '/web/users';
        return;
    }
    $('#userId').val(userId);
    $('#passwordForm').on('submit', function(e) {
        e.preventDefault();
        var pwd = $('#newPassword').val();
        var confirm = $('#confirmPassword').val();
        if (pwd !== confirm) {
            $('#passwordMsg').text('Passwords do not match.').addClass('text-danger').removeClass('text-success');
            return false;
        }
        if (!pwd) return;
        $.ajax({
            url: '/api/v1/users/' + userId + '/password',
            type: 'POST',
            contentType: 'application/json',
            data: JSON.stringify({ new_password: pwd }),
            success: function() {
                $('#passwordMsg').text('Password changed successfully.').addClass('text-success').removeClass('text-danger');
                $('#newPassword').val('');
                $('#confirmPassword').val('');
            },
            error: function() {
                $('#passwordMsg').text('Error changing password.').addClass('text-danger').removeClass('text-success');
            }
        });
    });
});
