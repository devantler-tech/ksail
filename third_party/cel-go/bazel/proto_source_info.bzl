"""Build rule for preserving source information in proto descriptor sets."""

load("@com_google_protobuf//bazel/common:proto_info.bzl", "ProtoInfo")

def _source_info_proto_descriptor_set(ctx):
    """Returns a proto descriptor set with source information preserved."""
    srcs = depset([s for dep in ctx.attr.proto_libs for s in dep[ProtoInfo].direct_sources])
    deps = depset(transitive = [dep[ProtoInfo].transitive_descriptor_sets for dep in ctx.attr.proto_libs])

    src_files = srcs.to_list()
    dep_files = deps.to_list()

    args = ctx.actions.args()
    args.add("--descriptor_set_out=" + ctx.outputs.out.path)
    args.add("--include_imports")
    args.add("--include_source_info=true")
    args.add("--proto_path=.")
    args.add("--proto_path=" + ctx.configuration.genfiles_dir.path)
    args.add("--descriptor_set_in=" + ":".join([d.path for d in dep_files]))
    args.add_all(src_files)

    ctx.actions.run(
        executable = ctx.executable._protoc,
        inputs = src_files + dep_files,
        outputs = [ctx.outputs.out],
        arguments = [args],
        mnemonic = "SourceInfoProtoDescriptorSet",
        progress_message = "Generating proto descriptor set with source information for %{label}",
    )

source_info_proto_descriptor_set = rule(
    doc = """
Rule for generating a proto descriptor set for the transitive dependencies of proto libraries
with source information preserved.

This can dramatically increase the size of the descriptor set, so only use it
when necessary (e.g. for formatting documentation about a CEL environment).

Source info is only preserved for input files for each proto_library label in
protolibs. Transitive dependencies are included with source info stripped.
""",
    attrs = {
        "proto_libs": attr.label_list(providers = [[ProtoInfo]]),
        "_protoc": attr.label(
            default = "@com_google_protobuf//:protoc",
            executable = True,
            cfg = "exec",
        ),
    },
    outputs = {
        "out": "%{name}-transitive-descriptor-set-source-info.proto.bin",
    },
    implementation = _source_info_proto_descriptor_set,
)
