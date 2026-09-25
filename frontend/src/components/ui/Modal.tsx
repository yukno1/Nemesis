/**
 * 模态框 —— GSAP 缩放淡入出场，Esc/遮罩关闭。
 */
import { useEffect, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { gsap } from "@/lib/gsap";

interface ModalProps {
	open: boolean;
	title: string;
	onClose: () => void;
	children: ReactNode;
	/** 底部操作区 */
	footer?: ReactNode;
	width?: string;
}

export function Modal({
	open,
	title,
	onClose,
	children,
	footer,
	width = "max-w-lg",
}: ModalProps) {
	const overlayRef = useRef<HTMLDivElement>(null);
	const panelRef = useRef<HTMLDivElement>(null);

	// 始终保存最新的 onClose，避免它进入动画 effect 的依赖
	const onCloseRef = useRef(onClose);
	useEffect(() => {
		onCloseRef.current = onClose;
	});

	useEffect(() => {
		if (!open) return;
		// 入场：遮罩淡入 + 面板缩放上浮
		const ctx = gsap.context(() => {
			gsap.fromTo(
				overlayRef.current,
				{ opacity: 0 },
				{ opacity: 1, duration: 0.2 },
			);
			gsap.fromTo(
				panelRef.current,
				{ opacity: 0, y: 16, scale: 0.97 },
				{ opacity: 1, y: 0, scale: 1, duration: 0.3, ease: "power3.out" },
			);
		});
		const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
		window.addEventListener("keydown", onKey);
		return () => {
			ctx.revert();
			window.removeEventListener("keydown", onKey);
		};
	}, [open]);

	if (!open) return null;

	return createPortal(
		<div
			ref={overlayRef}
			className="fixed inset-0 z-50 flex items-center justify-center bg-void/80 p-4 backdrop-blur-sm"
			onMouseDown={(e) => e.target === e.currentTarget && onClose()}
		>
			<div
				ref={panelRef}
				className={`panel w-full ${width} max-h-[85vh] overflow-y-auto p-5 shadow-2xl`}
			>
				<div className="mb-4 flex items-center justify-between">
					<h2 className="text-base font-semibold text-bone">{title}</h2>
					<button
						type="button"
						className="text-ash transition-colors hover:text-bone"
						onClick={onClose}
						aria-label="关闭"
					>
						✕
					</button>
				</div>
				{children}
				{footer && <div className="mt-5 flex justify-end gap-2">{footer}</div>}
			</div>
		</div>,
		document.body,
	);
}
