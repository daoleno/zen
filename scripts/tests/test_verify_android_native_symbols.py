import importlib.util
import pathlib
import unittest


SCRIPT = pathlib.Path(__file__).parents[1] / "verify-android-native-symbols.py"
SPEC = importlib.util.spec_from_file_location("verify_android_native_symbols", SCRIPT)
verify_symbols = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(verify_symbols)


class AndroidNativeSymbolVerifierTests(unittest.TestCase):
    def test_parses_only_undefined_dynamic_symbol_names(self):
        output = """
Symbol table '.dynsym' contains 5 entries:
   Num:    Value          Size Type    Bind   Vis      Ndx Name
    28: 0000000000000000     0 NOTYPE  GLOBAL DEFAULT  UND shm_open
    29: 0000000000000000     0 FUNC    GLOBAL DEFAULT  UND memcpy@LIBC (2)
    30: 0000000000000000     0 NOTYPE  GLOBAL DEFAULT  UND shm_unlink
    31: 0000000000123456    24 FUNC    GLOBAL DEFAULT   12 shm_open
"""

        self.assertEqual(
            verify_symbols.parse_undefined_symbols(output),
            {"memcpy", "shm_open", "shm_unlink"},
        )

    def test_lock_declares_both_unsupported_posix_shm_imports(self):
        lock = SCRIPT.parents[1] / "app/modules/zen-terminal-vt/native.lock.json"

        self.assertEqual(
            verify_symbols.load_forbidden_symbols(lock),
            ("shm_open", "shm_unlink"),
        )


if __name__ == "__main__":
    unittest.main()
