$(document).ready(function() {
function getNodeName() {
    var match = window.location.pathname.match(/node\/([^/]+)\/properties/);
    return match ? decodeURIComponent(match[1]) : '';
}
function loadNodeProperties() {
    var node = getNodeName();
    $('#nodeName').text(node);
    $.getJSON('/api/v1/agents/' + node + '/properties', function(data) {
        var tbody = '';
        data.forEach(function(np) {
            tbody += '<tr>' +
                '<td data-prop-id="' + np.property_id + '"></td>' +
                '<td>' + (np.value || '') + '</td>' +
                '<td>' +
                    '<button class="btn btn-sm btn-info edit-btn" data-id="' + np.property_id + '"><i class="fas fa-edit"></i></button> ' +
                    '<button class="btn btn-sm btn-danger delete-btn" data-id="' + np.property_id + '"><i class="fas fa-trash"></i></button>' +
                '</td>' +
            '</tr>';
        });
        $('#nodePropertiesTable tbody').html(tbody);
        // Remplir les noms de propriété
        $.getJSON('/api/v1/properties', function(props) {
            $('#nodePropertiesTable tbody tr').each(function() {
                var propId = $(this).find('td[data-prop-id]').data('prop-id');
                var prop = props.find(function(p) { return p.id === propId; });
                $(this).find('td[data-prop-id]').text(prop ? prop.name : propId);
            });
        });
    });
}
function loadPropertyOptions() {
    $.getJSON('/api/v1/properties', function(props) {
        var options = '';
        props.forEach(function(p) {
            options += '<option value="' + p.id + '">' + p.name + '</option>';
        });
        $('#modalPropertySelect').html(options);
    });
}
$(function() {
    loadNodeProperties();
    $('#btnAddNodeProperty').click(function() {
        loadPropertyOptions();
        $('#modalPropertyId').val('');
        $('#modalPropertySelect').prop('disabled', false);
        $('#modalPropertyValue').val('');
        $('#nodePropertyModal').modal('show');
    });
    $(document).on('click', '.edit-btn', function() {
        var propId = $(this).data('id');
        var node = getNodeName();
        $.getJSON('/api/v1/agents/' + node + '/properties/' + propId, function(np) {
            loadPropertyOptions();
            $('#modalPropertyId').val(np.property_id);
            $('#modalPropertySelect').val(np.property_id).prop('disabled', true);
            $('#modalPropertyValue').val(np.value);
            $('#nodePropertyModal').modal('show');
        });
    });
    $(document).on('click', '.delete-btn', function() {
        if(confirm('Delete this association?')) {
            var propId = $(this).data('id');
            var node = getNodeName();
            $.ajax({
                url: '/api/v1/agents/' + node + '/properties/' + propId,
                type: 'DELETE',
                success: function() { loadNodeProperties(); }
            });
        }
    });
    $('#saveNodePropertyBtn').click(function(e) {
        e.preventDefault();
        var node = getNodeName();
        var propId = $('#modalPropertyId').val() || $('#modalPropertySelect').val();
        var value = $('#modalPropertyValue').val();
        var method = $('#modalPropertyId').val() ? 'PUT' : 'POST';
        var url = method === 'POST' ? '/api/v1/agents/' + node + '/properties' : '/api/v1/agents/' + node + '/properties/' + propId;
        var data = { property_id: parseInt(propId), value: value };
        $.ajax({
            url: url,
            type: method,
            contentType: 'application/json',
            data: JSON.stringify(data),
            success: function() {
                $('#nodePropertyModal').modal('hide');
                loadNodeProperties();
            }
        });
    });
});
});