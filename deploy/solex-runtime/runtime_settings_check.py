"""Check administrator runtime settings without changing effective production values."""
from api_client import ApiError


def check_runtime_settings(admin, ordinary):
    base = '/system/admin/settings'
    bounds = {'agent.max_iterations': (1, 200),
              'agent.collaboration_wait_timeout_seconds': (1, 60)}
    rows = {row['key']: row for row in admin.call('GET', base)}
    values = {}
    for key, (minimum, maximum) in bounds.items():
        row = rows.get(key, {})
        value = row.get('value')
        if type(value) is not int or not minimum <= value <= maximum:
            raise RuntimeError('Missing or invalid administrator runtime setting: ' + key)
        if row.get('requires_restart'):
            raise RuntimeError('Agent runtime setting incorrectly requires a restart')
        path = base + '/' + key
        # Re-save the effective value. Never temporarily lower a production
        # budget while unrelated users may be starting their own requests.
        admin.call('PUT', path, {'value': value})
        for invalid in (0, maximum + 1, 1.5):
            try:
                admin.call('PUT', path, {'value': invalid})
            except ApiError as exc:
                if exc.status != 400:
                    raise
            else:
                admin.call('PUT', path, {'value': value})
                raise RuntimeError('Invalid runtime setting was accepted: ' + key)
        for method, endpoint, data in (
            ('PUT', path, {'value': maximum}),
            ('DELETE', path, None),
        ):
            try:
                ordinary.call(method, endpoint, data)
            except ApiError as exc:
                if exc.status != 403:
                    raise
            else:
                admin.call('PUT', path, {'value': value})
                raise RuntimeError('Ordinary user changed administrator runtime settings')
        values[key] = value
    after = {row['key']: row['value'] for row in admin.call('GET', base)}
    if any(after.get(key) != value for key, value in values.items()):
        raise RuntimeError('Runtime configuration changed during validation')
    return {'passed': True, 'values': values, 'admin_save': True,
            'invalid_values_rejected': True, 'ordinary_writes_forbidden': True}
