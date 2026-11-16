import { Toaster as Sonner, type ToasterProps } from "sonner";

function Toaster({ ...props }: ToasterProps) {
  return (
    <Sonner
      richColors
      position="bottom-right"
      toastOptions={{
        classNames: {
          toast: "group font-sans",
        },
      }}
      {...props}
    />
  );
}

export { Toaster };
