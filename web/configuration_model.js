$(function() {
    // Affiche la modal d'upload
    $('#btn-upload-config-model').on('click', function() {
        $('#modal-upload-config-model').modal('show');
    });

    window.addEventListener('userReady', function() {
        if(window.currentUser && window.currentUser.role === 'admin'){
            $('#btn-upload-config-model').show();
        }
    });

    // Formatage des dates pour l'affichage
    function formatDate(dateStr) {
        if (!dateStr || dateStr === "0001-01-01T00:00:00Z") return "";
        var d = new Date(dateStr);
        if (isNaN(d.getTime())) return dateStr;
        return d.toLocaleString();
    }

    // Tableau en mode serverSide : pagination, tri de colonnes et recherche
    // sont gérés côté serveur via /api/v1/configuration_models?current=1&draw=...
    var dataTable = $('#config-models-table').DataTable({
        processing: true,
        serverSide: true,
        ajax: {
            url: '/api/v1/configuration_models?current=1',
            type: 'GET'
        },
        order: [[3, 'desc']],
        pageLength: 25,
        lengthMenu: [10, 25, 50, 100],
        columns: [
            { data: 'id', title: 'ID' },
            { data: 'name', title: 'Name' },
            { data: 'uploaded_by', title: 'Uploaded by' },
            {
                data: 'upload_date',
                title: 'Upload date',
                render: function(data, type) {
                    if (type === 'sort') return data;
                    return formatDate(data);
                }
            },
            {
                data: 'last_usage',
                title: 'Last call',
                render: function(data, type) {
                    if (type === 'sort') return data;
                    return formatDate(data);
                }
            },
            {
                data: null,
                title: 'Action',
                searchable: false,
                orderable: false,
                render: function(data, type, row) {
                    return '<button class="btn btn-info btn-sm btn-detail-config-model" data-id="' + row.name + '">Details</button> ' +
                        '<button class="btn btn-danger btn-sm delete-config-model" data-id="' + row.id + '">Delete</button>';
                }
            }
        ]
    });

    // Soumission du formulaire d'upload
    $('#form-upload-config-model').on('submit', function(e) {
        e.preventDefault();
        var formData = new FormData(this);
        $.ajax({
            url: '/api/v1/configuration_models?current=1',
            type: 'POST',
            data: formData,
            processData: false,
            contentType: false,
            success: function() {
                $('#modal-upload-config-model').modal('hide');
                dataTable.ajax.reload(null, false);
            },
            error: function(xhr) {
                alert('Failed to upload: ' + xhr.responseText);
            }
        });
    });

    // Suppression
    $('#config-models-table').on('click', '.delete-config-model', function() {
        var id = $(this).data('id');
        if (confirm('Remove this model ?')) {
            $.ajax({
                url: '/api/v1/configuration_models?id=' + id,
                type: 'DELETE',
                success: function() {
                    dataTable.ajax.reload(null, false);
                },
                error: function(xhr) {
                    alert('Failed to delete: ' + xhr.responseText);
                }
            });
        }
    });
});
