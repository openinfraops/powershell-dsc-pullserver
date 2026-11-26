$(function() {
    let usersTable = $('#users-table').DataTable({
        ajax: {
            url: '/api/v1/users',
            dataSrc: ''
        },
        columns: [
            { data: 'first_name' },
            { data: 'last_name' },
            { data: 'email' },
            { data: 'is_active', render: function(data) { return `<span class="badge badge-${data ? 'success' : 'danger'}">${data ? 'Yes' : 'No'}</span>`; } },
            { data: 'created_at', defaultContent: '' },
            { data: 'last_logon_date', defaultContent: '' },
            { data: null, orderable: false, render: function(data, type, row) {
                let html = `
                    <button class="btn btn-sm btn-info edit-user" data-id="${row.id}"><i class="fas fa-edit"></i></button>
                `;
                if (!row.source || row.source.toLowerCase() === 'local') {
                    html += `<button class="btn btn-sm btn-warning password-user" data-id="${row.id}"><i class="fas fa-key"></i></button>`;
                }
                html += `
                    <button class="btn btn-sm btn-danger delete-user" data-id="${row.id}"><i class="fas fa-trash"></i></button>
                    <button class="btn btn-sm btn-${row.is_active ? 'warning' : 'success'} toggle-active" data-id="${row.id}" data-active="${row.is_active}">
                        <i class="fas fa-${row.is_active ? 'ban' : 'check'}"></i>
                    </button>
                `;
                return html;
            }}
        ]
    });

    // Actions
    $('#users-table').on('click', '.delete-user', function() {
        if(confirm('Delete this user ?')) {
            $.ajax({
                url: '/api/v1/users/' + $(this).data('id'),
                type: 'DELETE',
                success: function() {
                    usersTable.ajax.reload(null, false);
                }
            });
        }
    });
    $('#users-table').on('click', '.toggle-active', function() {
        const id = $(this).data('id');
        const active = !$(this).data('active');
        $.post('/api/v1/users/' + id + '/active?active=' + (active ? '1' : '0'), function() {
            usersTable.ajax.reload(null, false);
        });
    });
    $('#users-table').on('click', '.edit-user', function() {
        window.location.href = '/web/user_edit?id=' + $(this).data('id');
    });
    $('#users-table').on('click', '.password-user', function() {
        window.location.href = '/web/user_password?id=' + $(this).data('id');
    });
});
