"""Loi tat: chay tool that trong skill .claude/skills/vps-diaocthongthai/scripts/vps.py
Dung y het: python vps.py run|ls|cat|get|put|sql|info ...
"""
import os
import runpy
import sys

_real = os.path.join(os.path.dirname(os.path.abspath(__file__)),
                     ".claude", "skills", "vps-diaocthongthai", "scripts", "vps.py")
sys.argv[0] = _real
runpy.run_path(_real, run_name="__main__")
