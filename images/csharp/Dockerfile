FROM mono:6.12

# Pre-warm the compiler so the first student run isn't cold
RUN echo 'class P { static void Main() { System.Console.WriteLine("warm"); } }' > /tmp/warm.cs && \
    mcs -nologo -out:/tmp/warm.exe /tmp/warm.cs && mono /tmp/warm.exe && \
    rm -f /tmp/warm.cs /tmp/warm.exe

# Create non-root user for security
RUN groupadd -r modigo && useradd -r -g modigo modigo && \
    mkdir -p /code && chown modigo:modigo /code

WORKDIR /code
USER modigo

# Default command (overridden by the runner's compile wrapper)
CMD ["sh", "-c", "mcs -nologo -out:/tmp/main.exe *.cs && mono /tmp/main.exe"]
