import unittest
from attribution_check import same_timestamp


class TimestampAcceptanceTests(unittest.TestCase):
    def test_database_local_timezone_is_same_instant(self):
        self.assertTrue(same_timestamp('2026-11-02T10:22:40.592592Z', '2026-11-02T18:22:40.592592+08:00'))

    def test_actual_time_change_still_fails(self):
        self.assertFalse(same_timestamp('2026-11-02T10:22:40.592592Z', '2026-11-02T18:22:41.592592+08:00'))

    def test_missing_invalid_or_unzoned_time_fails(self):
        for value in (None, '', 'not a timestamp', '2026-11-02T10:22:40.592592'):
            self.assertFalse(same_timestamp(value, '2026-11-02T10:22:40.592592Z'))
