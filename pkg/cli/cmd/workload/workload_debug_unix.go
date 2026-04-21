//go:build !windows

package workload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/siderolabs/gen/channel"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/api/machine"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"golang.org/x/term"
)

// stdinBufSize is the buffer size for reading stdin data.
const stdinBufSize = 1024

// debugStream abstracts the bidirectional streaming interface for debug container
// communication, avoiding a direct dependency on google.golang.org/grpc types.
type debugStream interface {
	Send(request *machine.DebugContainerRunRequest) error
	Recv() (*machine.DebugContainerRunResponse, error)
	CloseSend() error
}

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

	talosClient, err := talosclient.New(ctx,
		talosclient.WithEndpoints(nodeEndpoint),
		talosclient.WithConfig(talosConfig),
	)
	if err != nil {
		return fmt.Errorf("create Talos client: %w", err)
	}

	defer talosClient.Close() //nolint:errcheck

	nodeCtx := talosclient.WithNode(ctx, nodeEndpoint)

	imgName, err := pullImageOnTalosNode(nodeCtx, talosClient, image)
	if err != nil {
		return err
	}

	// Detach from the parent context so SIGINT is forwarded to the container
	// rather than cancelling the stream immediately.
	nodeCtx = context.WithoutCancel(nodeCtx)

	nodeCtx, cancel := context.WithCancel(nodeCtx)
	defer cancel()

	runStream, err := talosClient.DebugClient.ContainerRun(nodeCtx)
	if err != nil {
		return fmt.Errorf("create debug container stream: %w", err)
	}

	return streamDebugContainer(nodeCtx, cancel, runStream, imgName, args)
}

// pullImageOnTalosNode pulls the specified image on the target Talos node
// using the Talos machine API.
func pullImageOnTalosNode(
	ctx context.Context,
	talosClient *talosclient.Client,
	imageRef string,
) (string, error) {
	err := talosClient.ImagePull( //nolint:staticcheck // ImageServiceClient returns grpc types blocked by depguard
		ctx,
		common.ContainerdNamespace_NS_SYSTEM,
		imageRef,
	)
	if err != nil {
		return "", fmt.Errorf("pull image %q: %w", imageRef, err)
	}

	return imageRef, nil
}

func streamDebugContainer(
	ctx context.Context,
	cancel context.CancelFunc,
	stream debugStream,
	imageName string,
	args []string,
) error {
	defer cancel()

	stdinFd := int(os.Stdin.Fd()) //nolint:gosec
	isTTY := term.IsTerminal(stdinFd)

	err := sendContainerSpec(stream, imageName, args, isTTY)
	if err != nil {
		return err
	}

	if isTTY {
		oldState, termErr := term.MakeRaw(stdinFd)
		if termErr != nil {
			return fmt.Errorf("set terminal to raw mode: %w", termErr)
		}

		defer func() {
			if oldState != nil {
				_ = term.Restore(stdinFd, oldState)
			}
		}()
	}

	sendC := make(chan *machine.DebugContainerRunRequest, 100) //nolint:mnd

	setupSignalForwarding(ctx, sendC)

	stdinDone := startStdinReader(ctx, sendC)
	sendDone := startSendLoop(stream, sendC)

	var exitCode int32 = -1

	recvDone := startRecvLoop(ctx, stream, &exitCode)

	return waitForStreams(ctx, cancel, stream, sendC,
		stdinDone, sendDone, recvDone, &exitCode,
	)
}

// sendContainerSpec sends the initial container specification to the debug stream.
func sendContainerSpec(
	stream debugStream,
	imageName string,
	args []string,
	isTTY bool,
) error {
	err := stream.Send(&machine.DebugContainerRunRequest{
		Request: &machine.DebugContainerRunRequest_Spec{
			Spec: &machine.DebugContainerRunRequestSpec{
				Containerd: &common.ContainerdInstance{
					Driver:    common.ContainerDriver_CONTAINERD,
					Namespace: common.ContainerdNamespace_NS_SYSTEM,
				},
				ImageName: imageName,
				Args:      args,
				Profile:   machine.DebugContainerRunRequestSpec_PROFILE_PRIVILEGED,
				Tty:       isTTY,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("send container spec: %w", err)
	}

	return nil
}

// startRecvLoop receives messages from the debug stream and writes stdout data.
func startRecvLoop(
	ctx context.Context,
	stream debugStream,
	exitCode *int32,
) chan error {
	recvDone := make(chan error, 1)

	go func() {
		for {
			msg, recvErr := stream.Recv()
			if recvErr != nil {
				if ctx.Err() != nil {
					recvDone <- context.Canceled

					return
				}

				recvDone <- recvErr

				return
			}

			switch msg.GetResp().(type) {
			case *machine.DebugContainerRunResponse_StdoutData:
				if stdoutData := msg.GetStdoutData(); stdoutData != nil {
					_, _ = os.Stdout.Write(stdoutData)
				}
			case *machine.DebugContainerRunResponse_ExitCode:
				*exitCode = msg.GetExitCode()

				recvDone <- io.EOF

				return
			default:
				fmt.Fprintf(os.Stderr, "unknown message type %T\n", msg.GetResp())
			}
		}
	}()

	return recvDone
}

// waitForStreams waits for either stdin or recv to complete and cleans up.
//
//nolint:cyclop
func waitForStreams(
	_ context.Context,
	cancel context.CancelFunc,
	stream debugStream,
	sendC chan *machine.DebugContainerRunRequest,
	stdinDone chan error,
	sendDone chan error,
	recvDone chan error,
	exitCode *int32,
) error {
	select {
	case stdinErr := <-stdinDone:
		if stdinErr != nil && !errors.Is(stdinErr, io.EOF) {
			fmt.Fprintf(os.Stderr, "%s\n", stdinErr.Error())
		}

		cancel()
		close(sendC)

		if sendDone != nil {
			<-sendDone
		}

		closeErr := stream.CloseSend()
		if closeErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close send stream: %v\n", closeErr)
		}

		recvErr := <-recvDone
		if recvErr != nil && !errors.Is(recvErr, context.Canceled) && !errors.Is(recvErr, io.EOF) {
			return recvErr
		}

	case recvErr := <-recvDone:
		if recvErr != nil && !errors.Is(recvErr, context.Canceled) && !errors.Is(recvErr, io.EOF) {
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

	if *exitCode != -1 && *exitCode != 0 {
		return fmt.Errorf("%w: %d", ErrNonZeroExitCode, *exitCode)
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
					sendTermResize(ctx, msgC)

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

// sendTermResize sends a terminal resize message if stdin is a terminal.
func sendTermResize(ctx context.Context, msgC chan<- *machine.DebugContainerRunRequest) {
	stdinFd := int(os.Stdin.Fd()) //nolint:gosec

	if !term.IsTerminal(stdinFd) {
		return
	}

	width, height, sizeErr := term.GetSize(stdinFd)
	if sizeErr != nil {
		return
	}

	channel.SendWithContext(ctx, msgC,
		&machine.DebugContainerRunRequest{
			Request: &machine.DebugContainerRunRequest_TermResize{
				TermResize: &machine.DebugContainerTerminalResize{
					Width:  int32(width),  //nolint:gosec
					Height: int32(height), //nolint:gosec
				},
			},
		})
}

// startStdinReader reads from stdin and sends data to the debug container.
func startStdinReader(
	ctx context.Context,
	msgC chan<- *machine.DebugContainerRunRequest,
) chan error {
	reader, writer := io.Pipe()
	done := make(chan error)

	go func() {
		_, _ = io.Copy(writer, os.Stdin)
		_ = writer.Close()
	}()

	go func() {
		<-ctx.Done()

		_ = writer.Close()
	}()

	go func() {
		buf := make([]byte, stdinBufSize)

		for {
			bytesRead, readErr := reader.Read(buf)
			if readErr != nil {
				done <- readErr

				return
			}

			if bytesRead == 0 {
				continue
			}

			stdinData := append([]byte(nil), buf[:bytesRead]...)

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

// startSendLoop sends messages from msgC to the debug stream.
func startSendLoop(
	stream debugStream,
	msgC chan *machine.DebugContainerRunRequest,
) chan error {
	done := make(chan error, 1)

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
