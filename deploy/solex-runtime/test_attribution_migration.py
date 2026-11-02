"""Historical attribution is evidence-based, tenant-scoped, and name-bounded."""
import json
from pathlib import Path
import sqlite3
import unittest

MIGRATION = Path(__file__).resolve().parents[2] / 'weknora/migrations/sqlite/000053_knowledge_provenance.up.sql'


class AttributionMigrationTests(unittest.TestCase):
    def test_backfill_rejects_unrelated_failed_and_synthetic_events(self):
        with sqlite3.connect(':memory:') as db:
            db.executescript('''
                CREATE TABLE users(id TEXT, username TEXT);
                CREATE TABLE knowledges(id TEXT, tenant_id INTEGER);
                CREATE TABLE audit_logs(id INTEGER, tenant_id INTEGER, target_type TEXT,
                    target_id TEXT, actor_user_id TEXT, action TEXT, outcome TEXT, created_at TEXT, details TEXT);
                INSERT INTO users VALUES ('alice','Alice'),('bob','Bob');
                INSERT INTO knowledges VALUES ('known',1),('unknown',1),('machine',1),('long',1),('email',1);
            ''')
            def event(i, tenant, doc, actor, action, outcome, name=None):
                db.execute('INSERT INTO audit_logs VALUES (?,?,?,?,?,?,?,?,?)',
                           (i, tenant, 'knowledge', doc, actor, action, outcome, f'2026-11-01 01:00:{i:02d}',
                            json.dumps({'actor_name': name} if name else {})))
            event(1, 1, 'known', 'alice', 'knowledge.created', 'accepted')
            event(2, 1, 'known', 'bob', 'knowledge.updated', 'success')
            event(3, 2, 'known', 'alice', 'knowledge.updated', 'success')
            event(4, 1, 'known', 'alice', 'knowledge.updated', 'denied')
            event(5, 1, 'machine', 'system-1', 'knowledge.created', 'accepted')
            event(6, 1, 'long', 'bob', 'knowledge.created', 'accepted', '用' * 120)
            event(7, 1, 'email', 'alice', 'knowledge.created', 'accepted', 'private@example.invalid')
            db.executescript(MIGRATION.read_text())
            known = db.execute('SELECT created_by,created_by_name,last_modified_by,last_modified_by_name FROM knowledges WHERE id="known"').fetchone()
            self.assertEqual(known, ('alice', 'Alice', 'bob', 'Bob'))
            unknown = db.execute('SELECT created_by,last_modified_by,last_modified_at FROM knowledges WHERE id IN ("unknown","machine")').fetchall()
            self.assertEqual(unknown, [('', '', None), ('', '', None)])
            self.assertEqual(db.execute('SELECT created_by_name FROM knowledges WHERE id="long"').fetchone()[0], '用' * 100)
            self.assertEqual(db.execute('SELECT created_by_name FROM knowledges WHERE id="email"').fetchone()[0], '')
