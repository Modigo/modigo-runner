FROM eclipse-temurin:21-jdk-alpine

# Pre-warm JVM by compiling and running a trivial class
RUN mkdir -p /warmup && \
    echo 'public class Warmup { public static void main(String[] a) { System.out.println("warm"); } }' > /warmup/Warmup.java && \
    javac /warmup/Warmup.java -d /warmup && \
    java -cp /warmup Warmup && \
    rm -rf /warmup && \
    rm -rf /warmup/Warmup.class

WORKDIR /workspace

CMD ["sh", "-c", "javac Main.java 2>&1 && java Main"]
