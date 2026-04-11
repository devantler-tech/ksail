//go:build !windows

package workload

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/siderolabs/gen/channel"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"golang.org/x/term"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/devantler-tech/ksail/v6/pkg/fsutil"
)

// runTalosHostDebug launches a privileged debug container on a Talos node using the
// Talos gRPC DebugClient. This provides host-level access for in-node troubleshooting.
func runTalosHostDebug(
	ctx context.Context,
	nodeEndpoint string,
	talosconfigPath string,
	image string,
	args []string,
) error {
	expandedPath, err := fsutil.ExpandHomePath(talosconfigPath)
	if err != nil {
		return fmt.Errorf("expand talosconfig path: %w", err)
	}

	talosConfig, err := clientconfig.Open(expandedPath)
	if err != nil {
		return fmt.Errorf("open talosconfig %q: %w", expandedPath, err)
	}

	c, err := talosclient.New(ctx,
		talosclient.WithEndpoints(nodeEndpoint),
		talosclient.WithConfig(talosConfig),
	)
	if err != nil {
		return fmt.Errorf("create Talos client: %w", err)
	}

	defer c.Close() //nolint:errcheck

	nodeCtx := talosclient.WithNode(ctx, nodeEndpoint)

	// Pull the image on the target node.
	imgName, err := pullImageOnTalosNode(nodeCtx, c, image)
	if err != nil {
		return err
	}

	// Detach from the parent context so SIGINT is forwarded to the container
	// rather than cancelling the stream immediately.
	nodeCtx = context.WithoutCancel(nodeCtx)
	nodeCtx, cancel := context.WithCancel(nodeCtx)
	defer cancel()

	const grpcMaxMsgSize = 4 * 1024 * 1024 // 4 MiB

	runStream, err := c.DebugClient.ContainerRun(nodeCtx,
		grpc.MaxCallRecvMsgSize(grpcMaxMsgSize),
		grpc.MaxCallSendMsgSize(grpcMaxMsgSize),
	)
	if err != nil {
		return fmt.Errorf("create debug container stream: %w", err)
	}

	return streamDebugContainer(nodeCtx, cancel, runStream, imgName, args)
}

// pullImageOnTalosNode pulls the specified image on the target Talos node
// using the Talos machine API.
func pullImageOnTalosNode(
	ctx context.Context,
	c *talosclient.Client,
	imageRef string,
) (string, error) {
	err := c.ImagePull(ctx, common.ContainerdNamespace_NS_SYSTEM, imageRef)
	if err != nil {
		return "", fmt.Errorf("pull image %q: %w", imageRef, err)
	}

	return imageRef, nil
}

//nolint:gocyclo,cyclop
func streamDebugContainer(
	ctx context.Context,
	cancel context.CancelFunc,
	stream grpc.BidiStreamingClient[machine.DebugContainerRunRequest, machine.DebugContainerRunResponse],
	imageName string,
	args []string,
) error {
	defer cancel()

	isTTY := term.IsTerminal(int(os.Stdin.Fd()))

	ctrdInstance := &common.ContainerdInstance{
		Driver:    common.ContainerDriver_CONTAINERD,
		Namespace: common.ContainerdNamespace_NS_SYSTEM,
	}

	err := stream.Send(&machine.DebugContainerRunRequest{
		Request: &machine.DebugContainerRunRequest_Spec{
			Spec: &machine.DebugContainerRunRequestSpec{
				Containerd: ctrdInstance,
				ImageName:  imageName,
				Args:       args,
				Profile:    machine.DebugContainerRunRequestSpec_PROFILE_PRIVILEGED,
				Tty:        isTTY,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("send container spec: %w", err)
	}

	if isTTY {
		oldState, termErr := term.MakeRaw(int(os.Stdin.Fd()))
		if termErr != nil {
			return fmt.Errorf("set terminal to raw mode: %w", termErr)
		}

		defer func() {
			if oldState != nil {
				term.Restore(int(os.Stdin.Fd()), oldState) //nolint:errcheck
			}
		}()
	}

	sendC := make(chan *machine.DebugContainerRunRequest, 100) //nolint:mnd

	setupSignalForwarding(ctx, sendC)

	stdinDone := startStdinReader(ctx, sendC)
	sendDone := startSendLoop(stream, sendC)
	recvDone := make(chan error, 1)

	var exitCode int32 = -1

	go func() {
		for {
			msg, recvErr := stream.Recv()
			if recvErr != nil {
				if status.Code(recvErr) == codes.Canceled {
					recvDone <- context.Canceled

					return
				}

				recvDone <- recvErr

				return
			}

			switch msg.Resp.(type) {
			case *machine.DebugContainerRunResponse_StdoutData:
				if stdoutData := msg.GetStdoutData(); stdoutData != nil {
					os.Stdout.Write(stdoutData) //nolint:errcheck
				}
			case *machine.DebugContainerRunResponse_ExitCode:
				exitCode = msg.GetExitCode()
				recvDone <- io.EOF

				return
			default:
				fmt.Fprintf(os.Stderr, "unknown message type %T\n", msg.Resp)
			}
		}
	}()

	select {
	case stdinErr := <-stdinDone:
		if stdinErr != nil && stdinErr != io.EOF {
			fmt.Fprintf(os.Stderr, "%s\n", stdinErr.Error())
		}

		cancel()
		close(sendC)

		if sendDone != nil {
			<-sendDone
		}

		if closeErr := stream.CloseSend(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close send stream: %v\n", closeErr)
		}

		if recvErr := <-recvDone; recvErr != nil && recvErr != io.EOF {
			return recvErr
		}

	case recvErr := <-recvDone:
		if recvErr != nil && recvErr != context.Canceled && recvErr != io.EOF {
			return recvErr
		}

		cancel()

		if stdinDone != nil {
			<-stdinDone
		}

		close(sendC)

		if sendDone != nil {
			<-sendDone
		}
	}

	if exitCode != -1 && exitCode != 0 {
		return fmt.Errorf("container exited with code %d", exitCode)
	}

	return nil
}

// setupSignalForwarding forwards Unix signals to the debug container. SIGWINCH
// triggers terminal resize messages.
func setupSignalForwarding(ctx context.Context, msgC chan<- *machine.DebugContainerRunRequest) {
	sigC := make(chan os.Signal, 1)

	forwardedSignals := []os.Signal{
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		syscall.SIGHUP,
		syscall.SIGWINCH,
	}

	signal.Notify(sigC, forwardedSignals...)

	go func() {
		defer signal.Stop(sigC)

		for {
			select {
			case <-ctx.Done():
				return
			case sig := <-sigC:
				unixSig, ok := sig.(syscall.Signal)
				if !ok {
					continue
				}

				if unixSig == syscall.SIGWINCH {
					if term.IsTerminal(int(os.Stdin.Fd())) {
						width, height, sizeErr := term.GetSize(int(os.Stdin.Fd()))
						if sizeErr != nil {
							continue
						}

						if !channel.SendWithContext(ctx, msgC,
							&machine.DebugContainerRunRequest{
								Request: &machine.DebugContainerRunRequest_TermResize{
									TermResize: &machine.DebugContainerTerminalResize{
										Width:  int32(width),  //nolint:gosec
										Height: int32(height), //nolint:gosec
									},
								},
							}) {
							return
						}
					}

					continue
				}

				if !channel.SendWithContext(ctx, msgC, &machine.DebugContainerRunRequest{
					Request: &machine.DebugContainerRunRequest_Signal{
						Signal: int32(unixSig), //nolint:gosec
					},
				}) {
					return
				}
			}
		}
	}()

	// Trigger an initial resize.
	sigC <- syscall.SIGWINCH
}

// startStdinReader reads from stdin and sends data to the debug container.
func startStdinReader(ctx context.Context, msgC chan<- *machine.DebugContainerRunRequest) chan error {
	r, w := io.Pipe()
	done := make(chan error)

	go func() {
		io.Copy(w, os.Stdin) //nolint:errcheck
		w.Close()            //nolint:errcheck
	}()

	go func() {
		<-ctx.Done()
		w.Close() //nolint:errcheck
	}()

	go func() {
		buf := make([]byte, 1024)

		for {
			n, readErr := r.Read(buf)
			if readErr != nil {
				done <- readErr

				return
			}

			if n == 0 {
				continue
			}

			stdinData := append([]byte(nil), buf[:n]...)

			if !channel.SendWithContext(ctx, msgC, &machine.DebugContainerRunRequest{
				Request: &machine.DebugContainerRunRequest_StdinData{
					StdinData: stdinData,
				},
			}) {
				done <- ctx.Err()

				return
			}
		}
	}()

	return done
}

// startSendLoop sends messages from msgC to the gRPC stream.
func startSendLoop(
	stream grpc.BidiStreamingClient[machine.DebugContainerRunRequest, machine.DebugContainerRunResponse],
	msgC chan *machine.DebugContainerRunRequest,
) chan error {
	done := make(chan error)

	go func() {
		for msg := range msgC {
			sendErr := stream.Send(msg)
			if sendErr != nil {
				done <- sendErr

				return
			}
		}

		done <- nil
	}()

	return done
}
