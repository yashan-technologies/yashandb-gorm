#!/usr/bin/python
# -*- coding: UTF-8 -*-

import os
import sys
import shutil
import zipfile
import argparse
import platform


# yapf: disable
class HelpFormatter(argparse.ArgumentDefaultsHelpFormatter):
    def __init__(self, prog, indent_increment=1, max_help_position=48, width=256):
        super(HelpFormatter, self).__init__(prog, indent_increment=indent_increment,
                                            max_help_position=max_help_position, width=width)


def get_parser():
    parser = argparse.ArgumentParser(
        description="building tool",
        prog="build.py",
        usage='%(prog)s [-h]',
        formatter_class=argparse.HelpFormatter,
    )
    subparser = parser.add_subparsers(help="sub action command: ")
    set_package_argument(subparser)

    return parser

# yapf: disable
def set_package_argument(subparser):
    sp = subparser.add_parser("package", help="package the sources code.", formatter_class=HelpFormatter)
    sp.add_argument("-o", "--output", help="The package will output to this path.")
    sp.set_defaults(func=package)

def get_project_path():
    return os.path.dirname(os.path.abspath(__file__))

class Ziper(object):

    def __init__(self, src_path, dest_path, name, ex_prefixs = None, ex_postfixs = None):
        self.src_path = src_path
        self.dest_path = dest_path
        self.name = name
        self.ex_prefixs = ex_prefixs
        self.ex_postfixs = ex_postfixs

    def genPkgName(self):
        return "{}.zip".format(self.name)

    def isExclude(self, f):
        if self.ex_prefixs:
            for prefix in self.ex_prefixs:
                if f.startswith(prefix):
                    return True
        if self.ex_postfixs:
            for postfix in self.ex_postfixs:
                if f.endswith(postfix):
                    return True
        return False

    def pack(self):
        return self.windowsPack()

    def windowsPack(self):
        pkgName = self.genPkgName()
        p, n = os.path.split(self.src_path)
        os.chdir(os.path.abspath(p))
        handler = zipfile.ZipFile(os.path.join(self.dest_path, pkgName), "w")
        for root, dirs, fs in os.walk(n):
            for f in fs:
                if self.isExclude(os.path.join(root, f)):
                    continue
                print(os.path.join(root, f))
                handler.write(os.path.join(root, f))
        handler.close()
        return 0

def get_version():
    if platform.system() == 'Windows':
        null_path = 'nul'
    else:
        null_path = '/dev/null'

    os.chdir(get_project_path())
    describe = "".join(os.popen("git describe --tags 2>{}".format(null_path)).readlines()).strip()
    return describe if describe else "unknow"

def move(src, dest):
    dest_path, dest_name = os.path.split(dest)
    if not os.path.exists(dest_path):
        os.makedirs(dest_path)
    shutil.move(src, dest)

def package(args):
    proj_path = get_project_path()
    src_path = proj_path
    output = proj_path if not args.output else args.output
    packge_version = get_version()
    if Ziper(
        src_path,
        output,
        "yashandb-gorm-{}".format(packge_version),
        ex_prefixs=("gorm-yasdb/.", "gorm-yasdb/clauses/."),
        ex_postfixs=(".tar.gz", ".zip")
        ).pack() != 0:
        print("compress package failed")
        return 1
    return 0

if __name__ == "__main__":
    parser = get_parser()
    args = parser.parse_args()
    if len(sys.argv) <= 1:
        parser.print_help()
        sys.exit(1)
    sys.exit(args.func(args))
