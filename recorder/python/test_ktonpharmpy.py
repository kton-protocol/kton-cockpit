"""ktonpharmpy without pharmpy: a fake modeling module with the same shape (python3 -m unittest)."""
import json
import os
import sys
import types
import unittest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import ktonpharmpy  # noqa: E402


class Model:
    def __init__(self, name, code):
        self.name, self.code, self.dataset = name, code, None


def fake_modeling():
    m = types.ModuleType("pharmpy.modeling")

    def read_model(path):
        return Model("run1", "$PROBLEM base")

    def add_lag_time(model):
        return set_name(Model(model.name, model.code + "\nALAG"), model.name)  # calls another function

    def set_name(model, name):
        return Model(name, model.code)

    def write_model(model, path="", force=False):
        return model

    def get_number_of_peripheral_compartments(model):
        return 0

    for f in (read_model, add_lag_time, set_name, write_model, get_number_of_peripheral_compartments):
        setattr(m, f.__name__, f)
    m.Model = Model  # a class: not wrapped
    return m


class TestKtonPharmpy(unittest.TestCase):
    def test_outer_calls_only_with_model_hashes(self):
        lines = []
        mod = fake_modeling()
        self.assertTrue(ktonpharmpy.wrap_modeling(mod, lambda *a: lines.append(a)))
        self.assertFalse(ktonpharmpy.wrap_modeling(mod, lambda *a: lines.append(a)))  # once
        m1 = mod.read_model("model/run1.ctl")
        m2 = mod.add_lag_time(m1)
        mod.get_number_of_peripheral_compartments(m2)  # takes a model, gives none: still written (in)
        mod.write_model(mod.set_name(m2, "run7"), "model/run7.ctl")
        recs = [(op, path, json.loads(extra)) for op, _, path, extra in lines]
        fns = [r[2]["fn"] for r in recs]
        self.assertEqual(fns, ["read_model", "add_lag_time", "get_number_of_peripheral_compartments", "set_name", "write_model"])
        self.assertTrue(recs[0][1].endswith("model/run1.ctl") and os.path.isabs(recs[0][1]))
        self.assertEqual(recs[1][2]["in"], [recs[0][2]["out"]])  # add_lag_time took what read_model gave
        self.assertEqual(recs[4][2]["in"], [recs[3][2]["out"]])
        self.assertNotIn("out", recs[4][2])
        self.assertTrue(recs[4][1].endswith("model/run7.ctl"))
        self.assertIs(mod.Model, Model)

    def test_a_failing_recorder_never_changes_the_result(self):
        mod = fake_modeling()

        def broken(*a):
            raise OSError("log gone")

        ktonpharmpy.wrap_modeling(mod, broken)
        self.assertEqual(mod.set_name(Model("a", "x"), "b").name, "b")

    def test_hash_ignores_identity_and_sees_code_and_name(self):
        h = ktonpharmpy.model_hash
        self.assertEqual(h(Model("a", "x")), h(Model("a", "x")))
        self.assertNotEqual(h(Model("a", "x")), h(Model("a", "y")))
        self.assertNotEqual(h(Model("a", "x")), h(Model("b", "x")))
        self.assertIsNone(h(object()))


if __name__ == "__main__":
    unittest.main()
